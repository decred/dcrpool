// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrpool/database"
	"github.com/decred/dcrpool/util"
)

// ErrWorkAlreadyExists is returned when an already existing work is found for
// the provided work id.
func ErrWorkAlreadyExists(id []byte) error {
	return fmt.Errorf("work '%v' already exists", string(id))
}

// ErrWorkNotFound is returned when no work is found for the provided work id.
func ErrWorkNotFound(id []byte) error {
	return fmt.Errorf("work '%v' not found", string(id))
}

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

// AcceptedWorkID generates a unique id for the work accepted by the network.
func AcceptedWorkID(blockHash string, blockHeight uint32) []byte {
	heightE := hex.EncodeToString(util.HeightToBigEndianBytes(blockHeight))
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

// FetchAcceptedWork fetches the accepted work referenced by the provided id.
func FetchAcceptedWork(db *bolt.DB, id []byte) (*AcceptedWork, error) {
	var work AcceptedWork
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.WorkBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.WorkBkt)
		}
		v := bkt.Get(id)
		if v == nil {
			return database.ErrValueNotFound(id)
		}
		err := json.Unmarshal(v, &work)
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
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.WorkBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.WorkBkt)
		}

		// Do not persist already existing accepted work.
		id := []byte(work.UUID)
		v := bkt.Get(id)
		if v != nil {
			return ErrWorkAlreadyExists(id)
		}

		workBytes, err := json.Marshal(work)
		if err != nil {
			return err
		}

		return bkt.Put([]byte(work.UUID), workBytes)
	})
	return err
}

// Update persists modifications to an existing work.
func (work *AcceptedWork) Update(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.WorkBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.WorkBkt)
		}

		// Assert work associated with the provided id exists before updating.
		id := []byte(work.UUID)
		v := bkt.Get(id)
		if v == nil {
			return ErrWorkNotFound(id)
		}

		workBytes, err := json.Marshal(work)
		if err != nil {
			return err
		}

		return bkt.Put([]byte(work.UUID), workBytes)
	})
	return err
}

// Delete removes the associated accepted work from the database.
func (work *AcceptedWork) Delete(db *bolt.DB) error {
	return database.Delete(db, database.WorkBkt, []byte(work.UUID))
}

// ListMinedWork returns work data associated with blocks mined by
// the pool.
func ListMinedWork(db *bolt.DB) ([]*AcceptedWork, error) {
	minedWork := make([]*AcceptedWork, 0)
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		bkt := pbkt.Bucket(database.WorkBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.WorkBkt)
		}

		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var work AcceptedWork
			err := json.Unmarshal(v, &work)
			if err != nil {
				return err
			}

			if work.Confirmed {
				minedWork = append(minedWork, &work)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return minedWork, nil
}

// ListMinedWorkByAccount returns all mined work data on blocks mined by the
// provided pool account id.
func ListMinedWorkByAccount(db *bolt.DB, accountID string) ([]*AcceptedWork, error) {
	minedWork := make([]*AcceptedWork, 0)
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		bkt := pbkt.Bucket(database.WorkBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.WorkBkt)
		}

		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var work AcceptedWork
			err := json.Unmarshal(v, &work)
			if err != nil {
				return err
			}

			if strings.Compare(work.MinedBy, accountID) == 0 {
				minedWork = append(minedWork, &work)
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
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.WorkBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.WorkBkt)
		}

		toDelete := [][]byte{}
		cursor := bkt.Cursor()
		workHeightB := make([]byte, 8)
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			_, err := hex.Decode(workHeightB, k[:8])
			if err != nil {
				return err
			}

			workHeight := util.BigEndianBytesToHeight(workHeightB)
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
