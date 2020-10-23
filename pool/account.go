// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/decred/dcrd/crypto/blake256"
	bolt "go.etcd.io/bbolt"
)

// Account represents a mining pool account.
type Account struct {
	UUID      string `json:"uuid"`
	Address   string `json:"address"`
	CreatedOn uint64 `json:"createdon"`
}

// AccountID generates a unique id using provided address of the account.
func AccountID(address string) string {
	hasher := blake256.New()
	_, _ = hasher.Write([]byte(address))
	return hex.EncodeToString(hasher.Sum(nil))
}

// NewAccount creates a new account.
func NewAccount(address string) *Account {
	// Since an account's id is derived from the address an account
	// can be shared by multiple pool clients.
	return &Account{
		UUID:    AccountID(address),
		Address: address,
	}
}

// fetchAccount fetches the account referenced by the provided id.
func (db *BoltDB) fetchAccount(id string) (*Account, error) {
	const funcName = "fetchAccount"
	var account Account
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, accountBkt)
		if err != nil {
			return err
		}

		v := bkt.Get([]byte(id))
		if v == nil {
			desc := fmt.Sprintf("%s: no account found for id %s", funcName, id)
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

// persistAccount saves the account to the database.
func (db *BoltDB) persistAccount(acc *Account) error {
	const funcName = "persistAccount"
	return db.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, accountBkt)
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

		acc.CreatedOn = uint64(time.Now().Unix())

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

// deleteAccount purges the referenced account from the database.
func (db *BoltDB) deleteAccount(id string) error {
	return deleteEntry(db, accountBkt, id)
}
