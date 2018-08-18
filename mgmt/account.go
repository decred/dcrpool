package mgmt

import (
	"encoding/json"
	"time"

	"github.com/segmentio/ksuid"

	"github.com/coreos/bbolt"

	"dnldd/dcrpool/database"
)

const (
	// Activate represents an activated account.
	Active = "active"
	// Disabled represents a disabled user account.
	Disabled = "disabled"
	// Banned represents a banned user account.
	Banned = "banned"
)

// Account represents a mining pool user account. Miners will be required to
// have an account created in order to authenticate miners to the account.
type Account struct {
	UUID       string `json:"uuid"`
	Email      string `json:"email"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	Address    string `json:"address"`
	Status     string `json:"status"`
	CreatedOn  uint64 `json:"createdon"`
	ModifiedOn uint64 `json:"modifiedon"`
}

// NewAccount initializes a new account.
func NewAccount(email string, username string, password string) (*Account, error) {
	id, err := ksuid.NewRandom()
	if err != nil {
		return nil, err
	}
	hashedPass, err := database.BcryptHash(password)
	if err != nil {
		return nil, err
	}
	account := &Account{
		UUID:       id.String(),
		Email:      email,
		Username:   username,
		Password:   string(hashedPass),
		Status:     Disabled,
		CreatedOn:  uint64(time.Now().Unix()),
		ModifiedOn: 0,
	}
	return account, nil
}

// GetAccount fetches the account referenced by the provided id.
func GetAccount(db *bolt.DB, id []byte) (*Account, error) {
	var account *Account
	err := db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(database.AccountBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.AccountBkt)
		}
		v := bkt.Get(id)
		if v == nil {
			return database.ErrValueNotFound(id)
		}

		err := json.Unmarshal(v, account)
		return err
	})
	return account, err
}

// Create persists the account to the database.
func (acc *Account) Create(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(database.AccountBkt)
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

// Delete purges the account from the database.
func (acc *Account) Delete(db *bolt.DB, state bool) error {
	return database.Delete(db, database.AccountBkt, []byte(acc.UUID))
}
