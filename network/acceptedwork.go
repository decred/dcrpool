// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"encoding/json"

	bolt "github.com/coreos/bbolt"
	"github.com/dnldd/dcrpool/database"
)

// AcceptedWork represents an accepted work submission to the network.
type AcceptedWork struct {
	UUID              string `json:"uuid"`
	Nonce             string `json:"nonce"`
	Connected         bool   `json:"connected"`
	ConnectedAtHeight uint32 `json:"connectedatheight"`
}

// NewAcceptedWork creates an accepted work instance.
func NewAcceptedWork(nonceSpaceE string) *AcceptedWork {
	return &AcceptedWork{
		UUID:              nonceSpaceE,
		Nonce:             nonceSpaceE,
		Connected:         false,
		ConnectedAtHeight: 0,
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
		workBytes, err := json.Marshal(work)
		if err != nil {
			return err
		}

		return bkt.Put([]byte(work.UUID), workBytes)
	})
	return err
}

// Update persists the updated accepted work to the database.
func (work *AcceptedWork) Update(db *bolt.DB) error {
	return work.Create(db)
}

// Delete removes the associated accepted work from the database.
func (work *AcceptedWork) Delete(db *bolt.DB) error {
	return database.Delete(db, database.WorkBkt, []byte(work.UUID))
}

// PruneAcceptedWork removes all accepted work entries with heights less than
// or equal to the provided height.
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
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var work AcceptedWork
			err := json.Unmarshal(v, &work)
			if err != nil {
				return err
			}

			if work.Connected && (work.ConnectedAtHeight > 0 &&
				work.ConnectedAtHeight < height) {
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
