package dividend

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/coreos/bbolt"
	"github.com/decred/dcrd/dcrutil"

	"dnldd/dcrpool/database"
)

// Miner types lists all known DCR miners, in order of descending hash power
const (
	CPU           = "cpu"
	ObeliskDCR1   = "obeliskdcr1"
	WoodpeckerWB2 = "woodpeckerwb2"
	FFMinerD18    = "ffminerd18"
	InnosiliconD9 = "innosilicond9"
	IbelinkDSM6T  = "ibelinkdsm6t"
	AntiminerDR3  = "antminerdr3"
	StrongUU1     = "stronguu1"
	WhatsminerD1  = "whatsminerd1"
)

// MinerHashes is a map of all known DCR miners and their coressponding
// hashrates.
var MinerHashes = map[string]*big.Int{
	CPU:           new(big.Int).SetInt64(210E3),
	ObeliskDCR1:   new(big.Int).SetInt64(1.2E12),
	WoodpeckerWB2: new(big.Int).SetInt64(1.5E12),
	FFMinerD18:    new(big.Int).SetInt64(1.8E12),
	InnosiliconD9: new(big.Int).SetInt64(2.4E12),
	IbelinkDSM6T:  new(big.Int).SetInt64(6E12),
	AntiminerDR3:  new(big.Int).SetInt64(7.8E12),
	StrongUU1:     new(big.Int).SetInt64(11E12),
	WhatsminerD1:  new(big.Int).SetInt64(44E12),
}

// Convenience variables.
var (
	ZeroRat = new(big.Rat)
	ZeroInt = new(big.Int)
)

var (
	// PoolFeesK is the key used to track pool fee payouts.
	PoolFeesK = "fees"

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
	CPU:           new(big.Rat).SetFloat64(1.0), // Reserved for testing.
	ObeliskDCR1:   new(big.Rat).SetFloat64(1.0),
	WoodpeckerWB2: new(big.Rat).SetFloat64(1.25),
	FFMinerD18:    new(big.Rat).SetFloat64(1.5),
	InnosiliconD9: new(big.Rat).SetFloat64(2.0),
	IbelinkDSM6T:  new(big.Rat).SetFloat64(5),
	AntiminerDR3:  new(big.Rat).SetFloat64(6.5),
	WhatsminerD1:  new(big.Rat).SetFloat64(36.667),
}

// CalculatePoolTarget determines the target difficulty at which the provided
// hashrate can generate a pool share by the provided target time.
func CalculatePoolTarget(hashRate *big.Int, targetTimeSecs *big.Int) *big.Int {
	bigOne := big.NewInt(1)

	difficulty := new(big.Int).Div(
		new(big.Int).Mul(hashRate, targetTimeSecs),
		new(big.Int).Lsh(bigOne, 32))

	// the difficulty is incremented by 1 to compensate for rounding-off errors.
	target := new(big.Int).Div(
		new(big.Int).Sub(new(big.Int).Lsh(bigOne, 255), bigOne),
		new(big.Int).Add(difficulty, bigOne))
	return target
}

// Share represents verifiable work performed by a pool client.
type Share struct {
	Account   string   `json:"account"`
	Weight    *big.Rat `json:"weight"`
	CreatedOn int64    `json:"createdOn"`
}

// NewShare creates a shate with the provided account and weight.
func NewShare(account string, weight *big.Rat) *Share {
	return &Share{
		Account:   account,
		Weight:    weight,
		CreatedOn: time.Now().UnixNano(),
	}
}

// ErrNotSupported is returned when an entity does not support an action.
func ErrNotSupported(entity, action string) error {
	return fmt.Errorf("action (%v) not supported for entity (%v)",
		action, entity)
}

// NanoToBigEndianBytes returns an 8-byte big endian representation of
// the provided nanosecond time.
func NanoToBigEndianBytes(nano int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(nano))
	return b
}

// BigEndianBytesToNano returns nanosecond time of the provided 8-byte big
// endian representation.
func BigEndianBytesToNano(b []byte) int64 {
	return int64(binary.BigEndian.Uint64(b[0:8]))
}

// Create persists a share to the database.
func (s *Share) Create(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.ShareBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.ShareBkt)
		}
		sBytes, err := json.Marshal(s)
		if err != nil {
			return err
		}
		err = bkt.Put(NanoToBigEndianBytes(s.CreatedOn), sBytes)
		return err
	})
	return err
}

// Update is not supported for shares.
func (s *Share) Update(db *bolt.DB) error {
	return ErrNotSupported("share", "update")
}

// Delete is not supported for shares.
func (s *Share) Delete(db *bolt.DB, state bool) error {
	return ErrNotSupported("share", "delete")
}

// ErrDivideByZero is returned the divisor of division operation is zero.
func ErrDivideByZero() error {
	return fmt.Errorf("divide by zero: divisor is zero")
}

// FetchEligibleShares fetches all shares within the provided inclusive bounds.
func FetchEligibleShares(db *bolt.DB, min []byte, max []byte) ([]*Share, error) {
	eligibleShares := make([]*Share, 0)
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.ShareBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.ShareBkt)
		}

		c := bkt.Cursor()
		for k, v := c.Seek(min); k != nil && bytes.Compare(k, max) <= 0; k, v = c.Next() {
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

// CalculateSharePercentages calculates the percentages due each account according
// to their weighted shares.
func CalculateSharePercentages(shares []*Share) (map[string]*big.Rat, error) {
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
			return nil, ErrDivideByZero()
		}

		dividend := new(big.Rat).Quo(shareCount, totalShares)
		dividends[account] = dividend
	}

	return dividends, nil
}

// CalculatePayments calculates the payments due participating accounts.
func CalculatePayments(percentages map[string]*big.Rat, amount dcrutil.Amount, poolFee float64, estMaturity int64) ([]*Payment, error) {
	// Deduct pool fee from the amount to be shared.
	fee := amount.MulF64(poolFee)
	amtSansFees := amount - fee

	// Calculate each participating account's portion of the amount after fees.
	payments := make([]*Payment, 0)
	for id, percentage := range percentages {
		percent, _ := percentage.Float64()
		amt := amtSansFees.MulF64(percent)
		payments = append(payments, NewPayment(id, amt, estMaturity))
	}

	// Add a payout entry for pool fees.
	payments = append(payments, NewPayment(PoolFeesK, fee, estMaturity))

	return payments, nil
}
