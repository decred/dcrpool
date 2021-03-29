// Copyright (c) 2019-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"encoding/hex"

	"github.com/decred/dcrd/crypto/blake256"
)

var (
	// defaultAccountID is the default account id value.
	// It is derived from a zero hash.
	defaultAccountID = AccountID(zeroHash.String())
)

// Account represents a mining pool account.
type Account struct {
	UUID      string `json:"uuid"`
	Address   string `json:"address"`
	CreatedOn uint64 `json:"createdon"`
}

// AccountID generates a unique id using the provided address of the account.
func AccountID(address string) string {
	hasher := blake256.New()
	_, _ = hasher.Write([]byte(address))
	return hex.EncodeToString(hasher.Sum(nil))
}

// NewAccount creates a new account.
func NewAccount(address string) *Account {
	return &Account{
		UUID:    AccountID(address),
		Address: address,
	}
}
