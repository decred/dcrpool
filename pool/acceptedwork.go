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

// AcceptedWorkID generates a unique id for work accepted by the network.
func AcceptedWorkID(blockHash string, blockHeight uint32) string {
	var buf bytes.Buffer
	_, _ = buf.WriteString(hex.EncodeToString(heightToBigEndianBytes(blockHeight)))
	_, _ = buf.WriteString(blockHash)
	return buf.String()
}

// NewAcceptedWork creates an accepted work.
func NewAcceptedWork(blockHash string, prevHash string, height uint32,
	minedBy string, miner string) *AcceptedWork {
	return &AcceptedWork{
		UUID:      AcceptedWorkID(blockHash, height),
		BlockHash: blockHash,
		PrevHash:  prevHash,
		Height:    height,
		MinedBy:   minedBy,
		Miner:     miner,
		CreatedOn: time.Now().UnixNano(),
	}
}

// FetchAcceptedWork fetches the accepted work referenced by the provided id.
func FetchAcceptedWork(db *bolt.DB, id string) (*AcceptedWork, error) {
	const funcName = "FetchAcceptedWork"
	var work AcceptedWork
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, workBkt)
		if err != nil {
			return err
		}
		v := bkt.Get([]byte(id))
		if v == nil {
			desc := fmt.Sprintf("%s: no value for key %s", funcName, id)
			return dbError(ErrValueNotFound, desc)
		}
		err = json.Unmarshal(v, &work)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal accepted work: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &work, err
}

// Persist saves the accepted work to the database.
func (work *AcceptedWork) Persist(db *bolt.DB) error {
	const funcName = "AcceptedWork.Persist"
	return db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, workBkt)
		if err != nil {
			return err
		}

		// Do not persist already existing accepted work.
		id := []byte(work.UUID)
		v := bkt.Get(id)
		if v != nil {
			desc := fmt.Sprintf("%s: work %s already exists", funcName,
				work.UUID)
			return dbError(ErrValueFound, desc)
		}
		workBytes, err := json.Marshal(work)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal accepted "+
				"work bytes: %v", funcName, err)
			return dbError(ErrParse, desc)
		}

		err = bkt.Put(id, workBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist accepted work: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		return nil
	})
}

// Update persists modifications to an existing work.
func (work *AcceptedWork) Update(db *bolt.DB) error {
	const funcName = "AcceptedWork.Update"
	return db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, workBkt)
		if err != nil {
			return err
		}

		// Assert the work provided exists before updating.
		id := []byte(work.UUID)
		v := bkt.Get(id)
		if v == nil {
			desc := fmt.Sprintf("%s: work %s not found", funcName, work.UUID)
			return dbError(ErrValueNotFound, desc)
		}
		workBytes, err := json.Marshal(work)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal accepted "+
				"work bytes: %v", funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		err = bkt.Put(id, workBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist accepted work: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		return nil
	})
}

// Delete removes the associated accepted work from the database.
func (work *AcceptedWork) Delete(db *bolt.DB) error {
	return deleteEntry(db, workBkt, work.UUID)
}

// ListMinedWork returns work data associated with all blocks mined by the pool
// regardless of whether they are confirmed or not.
//
// List is ordered, most recent comes first.
func ListMinedWork(db *bolt.DB) ([]*AcceptedWork, error) {
	const funcName = "ListMinedWork"
	minedWork := make([]*AcceptedWork, 0)
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, workBkt)
		if err != nil {
			return err
		}

		cursor := bkt.Cursor()
		for k, v := cursor.Last(); k != nil; k, v = cursor.Prev() {
			var work AcceptedWork
			err := json.Unmarshal(v, &work)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to unmarshal accepted "+
					"work: %v", funcName, err)
				return poolError(ErrParse, desc)
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

// FetchUnconfirmedWork returns all work which is not confirmed as mined with
// height less than the provided height.
func FetchUnconfirmedWork(db *bolt.DB, height uint32) ([]*AcceptedWork, error) {
	toReturn := make([]*AcceptedWork, 0)
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, workBkt)
		if err != nil {
			return err
		}

		heightBE := heightToBigEndianBytes(height)
		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			heightB, err := hex.DecodeString(string(k[:8]))
			if err != nil {
				return err
			}

			if bytes.Compare(heightBE, heightB) > 0 {
				var work AcceptedWork
				err := json.Unmarshal(v, &work)
				if err != nil {
					return err
				}

				if !work.Confirmed {
					toReturn = append(toReturn, &work)
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return toReturn, nil
}
