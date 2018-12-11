package network

import (
	"encoding/hex"
	"encoding/json"

	"github.com/coreos/bbolt"

	"dnldd/dcrpool/database"
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

		encoded := make([]byte, hex.EncodedLen(len(work.Nonce[:])))
		_ = hex.Encode(encoded, work.Nonce[:])
		return bkt.Put(encoded, workBytes)
	})
	return err
}

// Update persists the updated accepted work to the database.
func (work *AcceptedWork) Update(db *bolt.DB) error {
	return work.Create(db)
}

// Delete is not supported for accepted work.
func (work *AcceptedWork) Delete(db *bolt.DB) error {
	encoded := make([]byte, hex.EncodedLen(len(work.Nonce[:])))
	_ = hex.Encode(encoded, work.Nonce[:])
	return database.Delete(db, database.WorkBkt, encoded)
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

		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, _ = cursor.Next() {
			var work AcceptedWork
			err := json.Unmarshal(v, &work)
			if err != nil {
				return err
			}

			if work.Connected && (work.ConnectedAtHeight > -1 &&
				work.ConnectedAtHeight < height) {
				err = cursor.Delete()
				if err != nil {
					return err
				}
			}
		}

		return nil
	})

	return err
}
