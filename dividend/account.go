// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dividend

import (
	"encoding/hex"
	"encoding/json"
	"time"

	bolt "github.com/coreos/bbolt"
	"github.com/dchest/blake256"
	"github.com/decred/dcrpool/database"
)

// Account represents an anonymous mining pool account.
type Account struct {
	UUID      string `json:"uuid"`
	Address   string `json:"address"`
	CreatedOn uint64 `json:"createdon"`
}

// AccountID geenrates an id using provided address of the account.
func AccountID(address string) *string {
	hasher := blake256.New()
	hasher.Write([]byte(address))
	id := hex.EncodeToString(hasher.Sum(nil))
	return &id
}

// NewAccount generates a new account.
func NewAccount(address string) (*Account, error) {
	id := AccountID(address)
	account := &Account{
		UUID:      *id,
		Address:   address,
		CreatedOn: uint64(time.Now().Unix()),
	}
	return account, nil
}

// FetchAccount fetches the account referenced by the provided id.
func FetchAccount(db *bolt.DB, id []byte) (*Account, error) {
	var account Account
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.AccountBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.AccountBkt)
		}
		v := bkt.Get(id)
		if v == nil {
			return database.ErrValueNotFound(id)
		}
		err := json.Unmarshal(v, &account)
		return err
	})
	if err != nil {
		return nil, err
	}

	return &account, err
}

// Create persists the account to the database.
func (acc *Account) Create(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.AccountBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.AccountBkt)
		}
		accBytes, err := json.Marshal(acc)
		if err != nil {
			return err
		}
		err = bkt.Put([]byte(acc.UUID), accBytes)
		return err
	})
	return err
}

// Update is not supported for accounts.
func (acc *Account) Update(db *bolt.DB) error {
	return ErrNotSupported("account", "update")
}

// Delete purges the referenced account from the database.
func (acc *Account) Delete(db *bolt.DB) error {
	return database.Delete(db, database.AccountBkt, []byte(acc.UUID))
}
