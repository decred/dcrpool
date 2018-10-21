package network

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/coreos/bbolt"

	"dnldd/dcrpool/database"
	"dnldd/dcrpool/dividend"
)

// AcceptedWork represents an accepted work submission to the network.
type AcceptedWork struct {
	Hash   string `json:"hash"`
	Height uint32 `json:"height"`
}

// NewAcceptedWork creates an accepted work instance.
func NewAcceptedWork(hash string, height uint32) *AcceptedWork {
	return &AcceptedWork{
		Hash:   hash,
		Height: height,
	}
}

// GenerateAcceptedWorkID prefixes the provided block hash with the provided
// block height to generate a unique id for the accepted work.
func GenerateAcceptedWorkID(hash string, height uint32) []byte {
	id := fmt.Sprintf("%v%v", height, hash)
	return []byte(id)
}

// GetAcceptedWork fetches the accepted work referenced by the provided id.
func GetAcceptedWork(db *bolt.DB, id []byte) (*AcceptedWork, error) {
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

		id := GenerateAcceptedWorkID(work.Hash, work.Height)
		err = bkt.Put(id, workBytes)
		return err
	})
	return err
}

// Update is not supported for accepted work.
func (work *AcceptedWork) Update(db *bolt.DB) error {
	return dividend.ErrNotSupported("accepted work", "update")
}

// Delete is not supported for accepted work.
func (work *AcceptedWork) Delete(db *bolt.DB, state bool) error {
	return dividend.ErrNotSupported("accepted work", "delete")
}

// PruneWork removes all accepted work entries with heights less than
// or equal to the provided height.
func PruneWork(db *bolt.DB, height uint32) error {
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.WorkBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.WorkBkt)
		}

		var err error
		cursor := bkt.Cursor()
		prefix := []byte(fmt.Sprintf("%v", height))
		toPrune := make([][]byte, 0)

		// Iterating and deleting in two steps to avoid:
		// https://github.com/boltdb/bolt/issues/620
		// TODO: test if this issue affects bbolt as well.
		for k, _ := cursor.First(); k != nil &&
			bytes.Compare(k[:len(prefix)], prefix) < 0; k, _ = cursor.Next() {
			toPrune = append(toPrune, k)
		}

		for _, k := range toPrune {
			err = bkt.Delete(k)
			if err != nil {
				break
			}
		}

		return err
	})
	return err
}
