package network

import (
	"encoding/json"

	"github.com/coreos/bbolt"

	"dnldd/dcrpool/database"
	"dnldd/dcrpool/dividend"
)

// AcceptedWork represents an accepted work submission to the network.
type AcceptedWork struct {
	Nonce             []byte `json:"nonce"`
	Connected         bool   `json:"connected"`
	ConnectedAtHeight int64  `json:"connectedatheight"`
}

// NewAcceptedWork creates an accepted work instance.
func NewAcceptedWork(nonce []byte) *AcceptedWork {
	return &AcceptedWork{
		Nonce:             nonce,
		Connected:         false,
		ConnectedAtHeight: -1,
	}
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

		err = bkt.Put(work.Nonce[:], workBytes)
		return err
	})
	return err
}

// Update persists the updated accepted work to the database.
func (work *AcceptedWork) Update(db *bolt.DB) error {
	return work.Create(db)
}

// Delete is not supported for accepted work.
func (work *AcceptedWork) Delete(db *bolt.DB, state bool) error {
	return dividend.ErrNotSupported("accepted work", "delete")
}

// PruneAcceptedWork removes all accepted work entries with heights less than
// or equal to the provided height.
func PruneAcceptedWork(db *bolt.DB, height int64) error {
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
		toPrune := make([][]byte, 0)

		// Iterating and deleting in two steps to avoid:
		// https://github.com/boltdb/bolt/issues/620
		// TODO: test if this issue affects bbolt as well.
		for k, v := cursor.First(); k != nil; k, _ = cursor.Next() {
			var work AcceptedWork
			err := json.Unmarshal(v, work)
			if err != nil {
				return err
			}

			if work.Connected && (work.ConnectedAtHeight > -1 &&
				work.ConnectedAtHeight < height) {
				toPrune = append(toPrune, k)
			}
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
