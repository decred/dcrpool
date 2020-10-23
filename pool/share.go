// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ShareWeights reprsents the associated weights for each known DCR miner.
// With the share weight of the lowest hash DCR miner (LHM) being 1, the
// rest were calculated as :
// 				(Hash of Miner X * Weight of LHM)/ Hash of LHM
var ShareWeights = map[string]*big.Rat{
	CPU:           new(big.Rat).SetFloat64(1.0), // Reserved for testing.
	ObeliskDCR1:   new(big.Rat).SetFloat64(1.0),
	InnosiliconD9: new(big.Rat).SetFloat64(2.182),
	AntminerDR3:   new(big.Rat).SetFloat64(7.091),
	AntminerDR5:   new(big.Rat).SetFloat64(31.181),
	WhatsminerD1:  new(big.Rat).SetFloat64(43.636),
}

// shareID generates a unique share id using the provided account and time
// created.
func shareID(account string, createdOn int64) string {
	var buf bytes.Buffer
	_, _ = buf.WriteString(hex.EncodeToString(nanoToBigEndianBytes(createdOn)))
	_, _ = buf.WriteString(account)
	return buf.String()
}

// Share represents verifiable work performed by a pool client.
type Share struct {
	UUID    string   `json:"uuid"`
	Account string   `json:"account"`
	Weight  *big.Rat `json:"weight"`
}

// NewShare creates a share with the provided account and weight.
func NewShare(account string, weight *big.Rat) *Share {
	return &Share{
		UUID:    shareID(account, time.Now().UnixNano()),
		Account: account,
		Weight:  weight,
	}
}

// persistShare saves a share to the database.
func (db *BoltDB) persistShare(s *Share) error {
	const funcName = "persistShare"
	return db.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, shareBkt)
		if err != nil {
			return err
		}
		sBytes, err := json.Marshal(s)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal share bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		err = bkt.Put([]byte(s.UUID), sBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist share entry: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		return nil
	})
}

// ppsEligibleShares fetches all shares created before or at the provided time.
func (db *BoltDB) ppsEligibleShares(max int64) ([]*Share, error) {
	funcName := "ppsEligibleShares"
	eligibleShares := make([]*Share, 0)
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, shareBkt)
		if err != nil {
			return err
		}
		c := bkt.Cursor()
		createdOnB := make([]byte, 8)
		maxB := nanoToBigEndianBytes(max)
		for k, v := c.First(); k != nil; k, v = c.Next() {
			_, err := hex.Decode(createdOnB, k[:16])
			if err != nil {
				desc := fmt.Sprintf("%s: unable to decode share "+
					"created-on bytes: %v", funcName, err)
				return dbError(ErrDecode, desc)
			}

			if bytes.Compare(createdOnB, maxB) <= 0 {
				var share Share
				err := json.Unmarshal(v, &share)
				if err != nil {
					desc := fmt.Sprintf("%s: unable to unmarshal share: %v",
						funcName, err)
					return dbError(ErrParse, desc)
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

// pplnsEligibleShares fetches all shares keyed greater than the provided
// minimum.
func (db *BoltDB) pplnsEligibleShares(min int64) ([]*Share, error) {
	funcName := "pplnsEligibleShares"
	eligibleShares := make([]*Share, 0)
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, shareBkt)
		if err != nil {
			return err
		}
		c := bkt.Cursor()
		createdOnB := make([]byte, 8)
		minB := nanoToBigEndianBytes(min)
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			_, err := hex.Decode(createdOnB, k[:16])
			if err != nil {
				desc := fmt.Sprintf("%s: unable to decode share "+
					"created-on bytes: %v", funcName, err)
				return dbError(ErrDecode, desc)
			}

			if bytes.Compare(createdOnB, minB) > 0 {
				var share Share
				err := json.Unmarshal(v, &share)
				if err != nil {
					desc := fmt.Sprintf("%s: unable to unmarshal "+
						"share: %v", funcName, err)
					return dbError(ErrParse, desc)
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

// pruneShares removes invalidated shares from the db.
func (db *BoltDB) pruneShares(minNano int64) error {
	funcName := "pruneShares"
	minB := nanoToBigEndianBytes(minNano)

	return db.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, shareBkt)
		if err != nil {
			return err
		}
		toDelete := [][]byte{}
		cursor := bkt.Cursor()
		createdOnB := make([]byte, 8)
		for k, _ := cursor.First(); k != nil; k, _ = cursor.Next() {
			_, err := hex.Decode(createdOnB, k[:16])
			if err != nil {
				desc := fmt.Sprintf("%s: unable to decode share created-on "+
					"bytes: %v", funcName, err)
				return dbError(ErrDecode, desc)
			}
			if bytes.Compare(minB, createdOnB) > 0 {
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
}
