// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"time"

	bolt "github.com/coreos/bbolt"
	"github.com/davecgh/go-spew/spew"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
)

var (
	// PPS represents the pay per share payment method.
	PPS = "pps"

	// PPLNS represents the pay per last n shares payment method.
	PPLNS = "pplns"
)

// ShareWeights reprsents the associated weights for each known DCR miner.
// With the share weight of the lowest hash DCR miner (LHM) being 1, the
// rest were calculated as :
// 				(Hash of Miner X * Weight of LHM)/ Hash of LHM
var ShareWeights = map[string]*big.Rat{
	CPU: new(big.Rat).SetFloat64(1.0), // Reserved for testing.
	// ObeliskDCR1:   new(big.Rat).SetFloat64(1.0),
	InnosiliconD9: new(big.Rat).SetFloat64(2.182),
	AntminerDR3:   new(big.Rat).SetFloat64(7.091),
	AntminerDR5:   new(big.Rat).SetFloat64(31.181),
	WhatsminerD1:  new(big.Rat).SetFloat64(43.636),
}

// calculatePoolDifficulty determines the difficulty at which the provided
// hashrate can generate a pool share by the provided target time.
func calculatePoolDifficulty(net *chaincfg.Params, hashRate *big.Int, targetTimeSecs *big.Int) *big.Rat {
	hashesPerTargetTime := new(big.Int).Mul(hashRate, targetTimeSecs)
	powLimit := net.PowLimit
	powLimitFloat, _ := new(big.Float).SetInt(powLimit).Float64()

	// The number of possible iterations is calculated as:
	//
	//    iterations := 2^(256 - floor(log2(pow_limit)))
	iterations := math.Pow(2, 256-math.Floor(math.Log2(powLimitFloat)))

	// The difficulty at which the provided hashrate can mine a block is
	// calculated as:
	//
	//    difficulty = (hashes_per_sec * target_in_seconds) / iterations
	difficulty := new(big.Rat).Quo(new(big.Rat).SetInt(hashesPerTargetTime),
		new(big.Rat).SetFloat64(iterations))

	// Clamp the difficulty to 1 if needed.
	if difficulty.Cmp(ZeroRat) == 0 || difficulty.Cmp(ZeroRat) < 0 {
		difficulty = new(big.Rat).SetInt64(1)
	}

	return difficulty
}

// DifficultyToTarget converts the provided difficulty to a target based on the
// active network.
func DifficultyToTarget(net *chaincfg.Params, difficulty *big.Rat) (*big.Rat, error) {
	powLimit := new(big.Rat).SetInt(net.PowLimit)

	// The corresponding target is calculated as:
	//
	//    target = pow_limit / difficulty
	//
	// The result is clamped to the pow limit if it exceeds it.
	target := new(big.Rat).Quo(powLimit, difficulty)

	if target.Cmp(powLimit) > 0 {
		target = powLimit
	}

	return target, nil
}

// calculatePoolTarget determines the target difficulty at which the provided
// hashrate can generate a pool share by the provided target time.
func calculatePoolTarget(net *chaincfg.Params, hashRate *big.Int, targetTimeSecs *big.Int) (*big.Rat, *big.Rat, error) {
	difficulty := calculatePoolDifficulty(net, hashRate, targetTimeSecs)
	target, err := DifficultyToTarget(net, difficulty)

	return target, difficulty, err
}

// Share represents verifiable work performed by a pool client.
type Share struct {
	Account   string   `json:"account"`
	Weight    *big.Rat `json:"weight"`
	CreatedOn int64    `json:"createdOn"`
}

// NewShare creates a share with the provided account and weight.
func NewShare(account string, weight *big.Rat) *Share {
	return &Share{
		Account:   account,
		Weight:    weight,
		CreatedOn: time.Now().UnixNano(),
	}
}

// fetchShareBucket is a helper function for getting the share bucket.
func fetchShareBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return nil, MakeError(ErrBucketNotFound, desc, nil)
	}
	bkt := pbkt.Bucket(shareBkt)
	if bkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(shareBkt))
		return nil, MakeError(ErrBucketNotFound, desc, nil)
	}

	return bkt, nil
}

// Create persists a share to the database.
func (s *Share) Create(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchShareBucket(tx)
		if err != nil {
			return err
		}

		sBytes, err := json.Marshal(s)
		if err != nil {
			return err
		}
		err = bkt.Put(nanoToBigEndianBytes(s.CreatedOn), sBytes)
		return err
	})
	return err
}

// Update is not supported for shares.
func (s *Share) Update(db *bolt.DB) error {
	desc := "share update not supported"
	return MakeError(ErrNotSupported, desc, nil)
}

// Delete is not supported for shares.
func (s *Share) Delete(db *bolt.DB) error {
	desc := "share deletion not supported"
	return MakeError(ErrNotSupported, desc, nil)
}

// PPSEligibleShares fetches all shares within the provided inclusive bounds.
func PPSEligibleShares(db *bolt.DB, min []byte, max []byte) ([]*Share, error) {
	eligibleShares := make([]*Share, 0)
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchShareBucket(tx)
		if err != nil {
			return err
		}

		c := bkt.Cursor()
		if min == nil {
			for k, v := c.First(); k != nil; k, v = c.Next() {
				var share Share
				err := json.Unmarshal(v, &share)
				if err != nil {
					return err
				}

				eligibleShares = append(eligibleShares, &share)
			}
		}

		if min != nil {
			for k, v := c.Seek(min); k != nil && bytes.Compare(k, max) <= 0; k, v = c.Next() {
				var share Share
				err := json.Unmarshal(v, &share)
				if err != nil {
					return err
				}

				eligibleShares = append(eligibleShares, &share)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return eligibleShares, err
}

// PPLNSEligibleShares fetches all shares keyed greater than the provided
// minimum.
func PPLNSEligibleShares(db *bolt.DB, min []byte) ([]*Share, error) {
	eligibleShares := make([]*Share, 0)
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchShareBucket(tx)
		if err != nil {
			return err
		}

		c := bkt.Cursor()
		for k, v := c.Last(); k != nil && bytes.Compare(k, min) > 0; k, v = c.Prev() {
			var share Share
			err := json.Unmarshal(v, &share)
			if err != nil {
				return err
			}

			eligibleShares = append(eligibleShares, &share)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return eligibleShares, err
}

// sharePercentages calculates the percentages due each account according
// to their weighted shares.
func sharePercentages(shares []*Share) (map[string]*big.Rat, error) {
	totalShares := new(big.Rat)
	tally := make(map[string]*big.Rat)
	dividends := make(map[string]*big.Rat)

	// Tally all share weights for each participation account.
	for _, share := range shares {
		totalShares = totalShares.Add(totalShares, share.Weight)
		if _, ok := tally[share.Account]; ok {
			tally[share.Account] = tally[share.Account].
				Add(tally[share.Account], share.Weight)
			continue
		}

		tally[share.Account] = share.Weight
	}

	// Calculate each participating account to be claimed.
	for account, shareCount := range tally {
		if tally[account].Cmp(ZeroRat) == 0 {
			return nil, MakeError(ErrDivideByZero, "division by zero", nil)
		}

		dividend := new(big.Rat).Quo(shareCount, totalShares)
		dividends[account] = dividend
	}

	return dividends, nil
}

// CalculatePayments calculates the payments due participating accounts.
func CalculatePayments(percentages map[string]*big.Rat, total dcrutil.Amount,
	poolFee float64, height uint32, estMaturity uint32) ([]*Payment, error) {
	// Deduct pool fee from the amount to be shared.
	fee := total.MulF64(poolFee)
	amtSansFees := total - fee

	// Calculate each participating account's portion of the amount after fees.
	payments := make([]*Payment, 0)
	for account, percentage := range percentages {
		percent, _ := percentage.Float64()
		amt := amtSansFees.MulF64(percent)
		payments = append(payments, NewPayment(account, amt, height, estMaturity))
	}

	// Add a payout entry for pool fees.
	payments = append(payments, NewPayment(poolFeesK, fee, height, estMaturity))

	return payments, nil
}

// PruneShares removes invalidated shares from the db.
func PruneShares(db *bolt.DB, minNano int64) error {
	minBytes := nanoToBigEndianBytes(minNano)
	err := db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchShareBucket(tx)
		if err != nil {
			return err
		}

		toDelete := [][]byte{}
		cursor := bkt.Cursor()
		for k, _ := cursor.First(); k != nil; k, _ = cursor.Next() {
			if bytes.Compare(minBytes, k) > 0 {
				toDelete = append(toDelete, k)
			}
		}

		for _, entry := range toDelete {
			err := bkt.Delete(entry)
			if err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

// PPLNSSharePercentages computes the current mining reward percentages
// due pool accounts based on work performed measured by the PPS payment scheme.
func PPSSharePercentages(db *bolt.DB, poolFee float64, height uint32) (map[string]*big.Rat, error) {
	now := time.Now()
	nowNano := nanoToBigEndianBytes(now.UnixNano())

	// Fetch the last payment created time.
	var lastPaymentTimeNano []byte
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
			return MakeError(ErrBucketNotFound, desc, nil)
		}

		v := pbkt.Get(lastPaymentCreatedOn)
		lastPaymentTimeNano = make([]byte, len(v))
		copy(lastPaymentTimeNano, v)

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Fetch all eligible shares for payment calculations.
	shares, err := PPSEligibleShares(db, lastPaymentTimeNano, nowNano)
	if err != nil {
		return nil, err
	}

	if len(shares) == 0 {
		return make(map[string]*big.Rat), nil
	}

	// Deduct pool fees and calculate the payment due each participating
	// account.
	percentages, err := sharePercentages(shares)
	if err != nil {
		return nil, err
	}

	log.Tracef("Share PPS percentages are: %v", spew.Sdump(percentages))
	return percentages, nil
}

// PPLNSSharePercentages computes the current mining reward percentages due pool
// accounts based on work performed measured by the PPLNS payment scheme.
func PPLNSSharePercentages(db *bolt.DB, poolFee float64, height uint32, periodSecs uint32) (map[string]*big.Rat, error) {
	now := time.Now()
	min := now.Add(-(time.Second * time.Duration(periodSecs)))
	minNano := nanoToBigEndianBytes(min.UnixNano())

	// Fetch all eligible shares within the specified period.
	shares, err := PPLNSEligibleShares(db, minNano)
	if err != nil {
		return nil, err
	}

	if len(shares) == 0 {
		return make(map[string]*big.Rat), nil
	}

	// Deduct pool fees and calculate the payment due each participating
	// account.
	percentages, err := sharePercentages(shares)
	if err != nil {
		return nil, err
	}

	log.Tracef("Share PPLNS percentages are: %v", spew.Sdump(percentages))
	return percentages, nil
}
