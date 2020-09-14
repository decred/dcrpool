// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrutil/v3"
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
	const funcName = "AcccountID"
	_, err := dcrutil.DecodeAddress(address, activeNet)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to decode address %s: %v",
			funcName, address, err)
		return "", poolError(ErrDecode, desc)
	}
	hasher := blake256.New()
	_, _ = hasher.Write([]byte(address))
	return hex.EncodeToString(hasher.Sum(nil)), nil
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
	const funcName = "fetchAccountBucket"
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(poolBkt))
		return nil, dbError(ErrBucketNotFound, desc)
	}
	bkt := pbkt.Bucket(accountBkt)
	if bkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(accountBkt))
		return nil, dbError(ErrBucketNotFound, desc)
	}
	return bkt, nil
}

// FetchAccount fetches the account referenced by the provided id.
func FetchAccount(db *bolt.DB, id []byte) (*Account, error) {
	const funcName = "FetchAccount"
	var account Account
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchAccountBucket(tx)
		if err != nil {
			return err
		}

		v := bkt.Get(id)
		if v == nil {
			desc := fmt.Sprintf("%s: no account found for id %s", funcName,
				string(id))
			return dbError(ErrValueNotFound, desc)
		}
		err = json.Unmarshal(v, &account)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal account: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &account, err
}

// Create persists the account to the database.
func (acc *Account) Create(db *bolt.DB) error {
	const funcName = "Account.Create"
	return db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchAccountBucket(tx)
		if err != nil {
			return err
		}

		// Do not persist already existing account.
		id := []byte(acc.UUID)
		v := bkt.Get(id)
		if v != nil {
			desc := fmt.Sprintf("%s: account %s already exists", funcName,
				acc.UUID)
			return dbError(ErrValueFound, desc)
		}
		accBytes, err := json.Marshal(acc)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal account bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		err = bkt.Put([]byte(acc.UUID), accBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist account entry: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		return nil
	})
}

// Delete purges the referenced account from the database.
func (acc *Account) Delete(db *bolt.DB) error {
	return deleteEntry(db, accountBkt, []byte(acc.UUID))
}
