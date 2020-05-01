// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/decred/dcrpool/pool/errors"
	bolt "go.etcd.io/bbolt"
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

	// An accepted work becomes mined work once it is confirmed by incoming
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

// AcceptedWorkID generates a unique id for work accepted by the network.
func AcceptedWorkID(blockHash string, blockHeight uint32) []byte {
	buf := bytes.Buffer{}
	buf.WriteString(hex.EncodeToString(heightToBigEndianBytes(blockHeight)))
	buf.WriteString(blockHash)
	return buf.Bytes()
}

// NewAcceptedWork creates an accepted work.
func NewAcceptedWork(blockHash string, prevHash string, height uint32,
	minedBy string, miner string) *AcceptedWork {
	return &AcceptedWork{
		UUID:      string(AcceptedWorkID(blockHash, height)),
		BlockHash: blockHash,
		PrevHash:  prevHash,
		Height:    height,
		MinedBy:   minedBy,
		Miner:     miner,
		CreatedOn: time.Now().UnixNano(),
	}
}

// fetchWorkBucket is a helper function for getting the work bucket.
func fetchWorkBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return nil, errors.MakeError(errors.ErrBucketNotFound, desc, nil)
	}
	bkt := pbkt.Bucket(workBkt)
	if bkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(workBkt))
		return nil, errors.MakeError(errors.ErrBucketNotFound, desc, nil)
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
			return errors.MakeError(errors.ErrValueNotFound, desc, nil)
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
			return errors.MakeError(errors.ErrWorkExists, desc, nil)
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
			return errors.MakeError(errors.ErrWorkNotFound, desc, nil)
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

// ListMinedWork returns work data associated with all blocks mined by the pool
// regardless of whether they are confirmed or not.
//
// List is ordered, most recent comes first.
func ListMinedWork(db *bolt.DB) ([]*AcceptedWork, error) {
	minedWork := make([]*AcceptedWork, 0)
	err := db.View(func(tx *bolt.Tx) error {
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

			minedWork = append(minedWork, &work)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return minedWork, nil
}
