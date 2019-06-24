// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	bolt "github.com/coreos/bbolt"
)

// AcceptedWork represents an accepted work submission to the network.
type AcceptedWork struct {
	UUID      string `json:"uuid"`
	BlockHash string `json:"blockhash"`
	PrevHash  string `json:"prevhash"`
	Height    uint32 `json:"height"`
	MinedBy   string `json:"minedby"`
	Miner     string `json:"miner"`
	CreatedOn int64  `json:"createdon"`

	// An accepted work becomes mined work once it is confirmed by an incoming
	// work as the parent block it was built on.
	Confirmed bool `json:"confirmed"`
}

// heightToBigEndianBytes returns a 4-byte big endian representation of
// the provided block height.
func heightToBigEndianBytes(height uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, height)
	return b
}

// bigEndianBytesToHeight returns the block height of the provided 4-byte big
// endian representation.
func bigEndianBytesToHeight(b []byte) uint32 {
	return binary.BigEndian.Uint32(b[0:4])
}

// AcceptedWorkID generates a unique id for the work accepted by the network.
func AcceptedWorkID(blockHash string, blockHeight uint32) []byte {
	heightE := hex.EncodeToString(heightToBigEndianBytes(blockHeight))
	id := fmt.Sprintf("%v%v", heightE, blockHash)
	return []byte(id)
}

// NewAcceptedWork creates an accepted work instance.
func NewAcceptedWork(blockHash string, prevHash string, height uint32, minedBy string, miner string) *AcceptedWork {
	id := AcceptedWorkID(blockHash, height)
	return &AcceptedWork{
		UUID:      string(id),
		BlockHash: blockHash,
		PrevHash:  prevHash,
		Height:    height,
		MinedBy:   minedBy,
		Miner:     miner,
		CreatedOn: time.Now().Unix(),
	}
}

// fetchWorkBucket is a helper function for getting the work bucket.
func fetchWorkBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return nil, MakeError(ErrBucketNotFound, desc, nil)
	}
	bkt := pbkt.Bucket(workBkt)
	if bkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(workBkt))
		return nil, MakeError(ErrBucketNotFound, desc, nil)
	}

	return bkt, nil
}

// FetchAcceptedWork fetches the accepted work referenced by the provided id.
func FetchAcceptedWork(db *bolt.DB, id []byte) (*AcceptedWork, error) {
	var work AcceptedWork
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchWorkBucket(tx)
		if err != nil {
			return err
		}

		v := bkt.Get(id)
		if v == nil {
			desc := fmt.Sprintf("no value for key %s", string(id))
			return MakeError(ErrValueNotFound, desc, nil)
		}

		err = json.Unmarshal(v, &work)
		return err
	})
	if err != nil {
		return nil, err
	}

	return &work, err
}

// Create persists the accepted work to the database.
func (work *AcceptedWork) Create(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchWorkBucket(tx)
		if err != nil {
			return err
		}

		// Do not persist already existing accepted work.
		id := []byte(work.UUID)
		v := bkt.Get(id)
		if v != nil {
			desc := fmt.Sprintf("work %s already exists", work.UUID)
			return MakeError(ErrWorkExists, desc, nil)
		}

		workBytes, err := json.Marshal(work)
		if err != nil {
			return err
		}

		return bkt.Put(id, workBytes)
	})
	return err
}

// Update persists modifications to an existing work.
func (work *AcceptedWork) Update(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchWorkBucket(tx)
		if err != nil {
			return err
		}

		// Assert the work provided exists before updating.
		id := []byte(work.UUID)
		v := bkt.Get(id)
		if v == nil {
			desc := fmt.Sprintf("work %s not found", work.UUID)
			return MakeError(ErrWorkNotFound, desc, nil)
		}

		workBytes, err := json.Marshal(work)
		if err != nil {
			return err
		}

		return bkt.Put(id, workBytes)
	})
	return err
}

// Delete removes the associated accepted work from the database.
func (work *AcceptedWork) Delete(db *bolt.DB) error {
	return deleteEntry(db, workBkt, []byte(work.UUID))
}

// ListMinedWork returns the N most recent work data associated with blocks
// mined by the pool.
//
// List is ordered, most recent comes first.
func ListMinedWork(db *bolt.DB, n int) ([]*AcceptedWork, error) {
	minedWork := make([]*AcceptedWork, 0)
	if n == 0 {
		return minedWork, nil
	}

	err := db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchWorkBucket(tx)
		if err != nil {
			return err
		}

		cursor := bkt.Cursor()
		for k, v := cursor.Last(); k != nil; k, v = cursor.Prev() {
			var work AcceptedWork
			err := json.Unmarshal(v, &work)
			if err != nil {
				return err
			}

			if work.Confirmed {
				minedWork = append(minedWork, &work)
				if len(minedWork) == n {
					return nil
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return minedWork, nil
}

// listMinedWorkByAccount returns the N most recent mined work data on
// blocks mined by the provided pool account id.
//
// List is ordered, most recent comes first.
func listMinedWorkByAccount(db *bolt.DB, accountID string, n int) ([]*AcceptedWork, error) {
	minedWork := make([]*AcceptedWork, 0)
	if n == 0 {
		return minedWork, nil
	}

	err := db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchWorkBucket(tx)
		if err != nil {
			return err
		}

		cursor := bkt.Cursor()
		for k, v := cursor.Last(); k != nil; k, v = cursor.Prev() {
			var work AcceptedWork
			err := json.Unmarshal(v, &work)
			if err != nil {
				return err
			}

			if strings.Compare(work.MinedBy, accountID) == 0 && work.Confirmed {
				minedWork = append(minedWork, &work)
				if len(minedWork) == n {
					return nil
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return minedWork, nil
}

// PruneAcceptedWork removes all accepted work not confirmed as mined work with
// heights less than the provided height.
func PruneAcceptedWork(db *bolt.DB, height uint32) error {
	err := db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchWorkBucket(tx)
		if err != nil {
			return err
		}

		toDelete := [][]byte{}
		cursor := bkt.Cursor()
		workHeightB := make([]byte, 8)
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			_, err := hex.Decode(workHeightB, k[:8])
			if err != nil {
				return err
			}

			workHeight := bigEndianBytesToHeight(workHeightB)
			if workHeight < height {
				var work AcceptedWork
				err := json.Unmarshal(v, &work)
				if err != nil {
					return err
				}

				// Only prune unconfirmed accepted work.
				if !work.Confirmed {
					toDelete = append(toDelete, k)
				}
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
