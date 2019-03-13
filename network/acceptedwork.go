// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	bolt "github.com/coreos/bbolt"

	"github.com/dnldd/dcrpool/database"
	"github.com/dnldd/dcrpool/dividend"
	"github.com/dnldd/dcrpool/util"
)

// ErrBucketNotFound is returned when a provided database bucket cannot be
// found.
func ErrWorkAlreadyExists(id []byte) error {
	return fmt.Errorf("work '%v' already exists", string(id))
}

// AcceptedWork represents an accepted work submission to the network.
type AcceptedWork struct {
	UUID      string `json:"uuid"`
	BlockHash string `json:"blockhash"`
	PrevHash  string `json:"prevhash"`
	Height    uint32 `json:"height"`
	MinedBy   string `json:"minedby"`
	Miner     string `json:"miner"`
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

// FetchMinedWork fetches the mined work referenced by the provided id.
func FetchMinedWork(db *bolt.DB, id []byte) (*AcceptedWork, error) {
	var work AcceptedWork
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.MinedBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.MinedBkt)
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

// PersistMinedWork stores details of a mined block by the pool to the db.
func (work *AcceptedWork) PersistMinedWork(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.MinedBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.MinedBkt)
		}

		workBytes, err := json.Marshal(work)
		if err != nil {
			return err
		}

		return bkt.Put([]byte(work.UUID), workBytes)
	})
	return err
}

// Update is not supported for accepted work.
func (work *AcceptedWork) Update(db *bolt.DB) error {
	return dividend.ErrNotSupported("accepted work", "update")
}

// Delete removes the associated accepted work from the database.
func (work *AcceptedWork) Delete(db *bolt.DB) error {
	return database.Delete(db, database.WorkBkt, []byte(work.UUID))
}

// DeleteMinedWork removes the associated mined work from the database.
func (work *AcceptedWork) DeleteMinedWork(db *bolt.DB) error {
	return database.Delete(db, database.MinedBkt, []byte(work.UUID))
}

// ListMinedWork returns mined work data associated with blocks mined by
// the pool.
func ListMinedWork(db *bolt.DB, page uint32) ([]*AcceptedWork, uint32, error) {
	minedWork := make([]*AcceptedWork, 0)
	numPages := uint32(0)
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		// return an empty list if the mined block counter has not been
		// initialized.
		v := pbkt.Get(database.MinedBlocks)
		if v == nil {
			return nil
		}

		var minedCount uint32
		if v != nil {
			minedCount = binary.LittleEndian.Uint32(v)
		}

		extraPage := minedCount%PageCount > 0
		numPages = minedCount / PageCount
		if extraPage {
			numPages++
		}

		// return an empty list if the page requested is greater than the
		// currently available number of pages.
		if page > numPages {
			return nil
		}

		bkt := pbkt.Bucket(database.MinedBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.MinedBkt)
		}

		cursor := bkt.Cursor()

		// Mark the index position to start reading from
		idx := page * PageCount
		count := 0
		iter := uint32(0)
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			// Skip to the start of the specified page.
			if iter != idx {
				iter++
				continue
			}

			var work AcceptedWork
			err := json.Unmarshal(v, &work)
			if err != nil {
				return err
			}

			minedWork = append(minedWork, &work)

			// Stop iterating when the number of mined work fetched is a full
			// page.
			count++
			if count == PageCount {
				break
			}
		}

		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	return minedWork, numPages, nil
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

		// return an empty list if the mined block counter is has not been
		// initialized.
		v := pbkt.Get(database.MinedBlocks)
		if v == nil {
			return nil
		}

		bkt := pbkt.Bucket(database.MinedBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.MinedBkt)
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

// FilterParentAcceptedWork locates the accepted work associated with the
// previous block hash of the provided accepted work. It also removes all
// invalidated accepted work at the same height.
func (work *AcceptedWork) FilterParentAcceptedWork(db *bolt.DB) (*AcceptedWork, error) {
	var prevWork AcceptedWork
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.WorkBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.WorkBkt)
		}

		heightB := util.HeightToBigEndianBytes(work.Height - 1)
		prefix := make([]byte, hex.EncodedLen(len(heightB)))
		hex.Encode(prefix, heightB)

		toDelete := [][]byte{}
		match := false
		prevHashB := []byte(work.PrevHash)
		cursor := bkt.Cursor()
		for k, v := cursor.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = cursor.Next() {
			parentWork := k[len(prefix):]
			if !match {
				if bytes.Equal(parentWork, prevHashB) {
					err := json.Unmarshal(v, &prevWork)
					if err != nil {
						return err
					}
					match = true
					continue
				}
			}

			if match {
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

	if err != nil {
		return nil, err
	}

	if prevWork.UUID == "" {
		return nil, err
	}

	return &prevWork, nil
}

// PruneAcceptedWork removes all accepted work with heights less than
// the provided height.
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
		for k, _ := cursor.First(); k != nil; k, _ = cursor.Next() {
			_, err := hex.Decode(workHeightB, k[:8])
			if err != nil {
				return err
			}

			workHeight := util.BigEndianBytesToHeight(workHeightB)
			if workHeight < height {
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
