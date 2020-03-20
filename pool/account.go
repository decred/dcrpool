// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrutil/v2"
	bolt "go.etcd.io/bbolt"
)

// Account represents a mining pool account.
type Account struct {
	UUID      string `json:"uuid"`
	Address   string `json:"address"`
	CreatedOn uint64 `json:"createdon"`
}

// AccountID generates a unique id using provided address of the account.
func AccountID(address string, activeNet *chaincfg.Params) (string, error) {
	_, err := dcrutil.DecodeAddress(address, activeNet)
	if err != nil {
		return "", err
	}

	hasher := blake256.New()
	_, err = hasher.Write([]byte(address))
	if err != nil {
		return "", err
	}

	id := hex.EncodeToString(hasher.Sum(nil))
	return id, nil
}

// NewAccount creates a new account.
func NewAccount(address string, activeNet *chaincfg.Params) (*Account, error) {
	// Since an account's id is derived from the address an account
	// can be shared by multiple pool clients.
	id, err := AccountID(address, activeNet)
	if err != nil {
		return nil, err
	}

	account := &Account{
		UUID:      id,
		Address:   address,
		CreatedOn: uint64(time.Now().Unix()),
	}

	return account, nil
}

// fetchAccountBucket is a helper function for getting the account bucket.
func fetchAccountBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return nil, MakeError(ErrBucketNotFound, desc, nil)
	}
	bkt := pbkt.Bucket(accountBkt)
	if bkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(accountBkt))
		return nil, MakeError(ErrBucketNotFound, desc, nil)
	}

	return bkt, nil
}

// FetchAccount fetches the account referenced by the provided id.
func FetchAccount(db *bolt.DB, id []byte) (*Account, error) {
	var account Account
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchAccountBucket(tx)
		if err != nil {
			return err
		}

		v := bkt.Get(id)
		if v == nil {
			desc := fmt.Sprintf("no account found for id %s", string(id))
			return MakeError(ErrValueNotFound, desc, nil)
		}

		err = json.Unmarshal(v, &account)
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
		bkt, err := fetchAccountBucket(tx)
		if err != nil {
			return err
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
	desc := "account update not supported"
	return MakeError(ErrNotSupported, desc, nil)
}

// Delete purges the referenced account from the database.
func (acc *Account) Delete(db *bolt.DB) error {
	return deleteEntry(db, accountBkt, []byte(acc.UUID))
}
