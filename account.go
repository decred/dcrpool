package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/coreos/bbolt"
	"github.com/segmentio/ksuid"
	"golang.org/x/crypto/bcrypt"

	"dnldd/dcrpool/database"
)

// Account represents a mining pool user account.
type Account struct {
	UUID       string `json:"uuid"`
	Name       string `json:"name"`
	Address    string `json:"address"`
	Pass       string `json:"pass,omitempty"`
	CreatedOn  uint64 `json:"createdon"`
	ModifiedOn uint64 `json:"modifiedon"`
}

// bcryptHash generates a bcrypt hash from the supplied plaintext. This should
// be used to hash passwords before persisting in a database.
func bcryptHash(plaintext string) ([]byte, error) {
	hashedPass, err := bcrypt.GenerateFromPassword([]byte(plaintext),
		bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash plaintext: %v", err)
	}
	return hashedPass, nil
}

// NewAccount generates a new account.
func NewAccount(name string, address string, pass string) (*Account, error) {
	id, err := ksuid.NewRandom()
	if err != nil {
		return nil, err
	}
	hashedPass, err := bcryptHash(pass)
	if err != nil {
		return nil, err
	}
	account := &Account{
		UUID:       id.String(),
		Name:       name,
		Address:    address,
		Pass:       string(hashedPass),
		CreatedOn:  uint64(time.Now().Unix()),
		ModifiedOn: 0,
	}
	return account, nil
}

// GetAccount fetches the account referenced by the provided id.
func GetAccount(db *bolt.DB, id []byte) (*Account, error) {
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

// Update persists the updated account to the database.
func (acc *Account) Update(db *bolt.DB) error {
	return acc.Create(db)
}

// Delete purges the referenced account from the database.
func (acc *Account) Delete(db *bolt.DB, state bool) error {
	return database.Delete(db, database.AccountBkt, []byte(acc.UUID))
}
