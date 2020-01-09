package pool

import (
	"fmt"
	"testing"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg/v2"
)

func persistAccount(db *bolt.DB, address string, activeNet *chaincfg.Params) (*Account, error) {
	acc, err := NewAccount(address, activeNet)
	if err != nil {
		return nil, fmt.Errorf("unable to create account: %v", err)
	}
	err = acc.Create(db)
	if err != nil {
		return nil, fmt.Errorf("unable to persist account: %v", err)
	}
	return acc, nil
}

func testAccount(t *testing.T, db *bolt.DB) {
	accountA, err := persistAccount(db, "Ssj6Sd54j11JM8qpenCwfwnKD73dsjm68ru",
		chaincfg.SimNetParams())
	if err != nil {
		t.Fatal(err)
	}

	accountB, err := persistAccount(db, "SssPc1UNr8czcP3W9hfAgpmLRa3zJPDhfSy",
		chaincfg.SimNetParams())
	if err != nil {
		t.Fatal(err)
	}

	// Fetch an account with its id.
	fetchedAccount, err := FetchAccount(db, []byte(accountA.UUID))
	if err != nil {
		t.Fatalf("FetchAccount error: %v", err)
	}

	if fetchedAccount.Address != accountA.Address {
		t.Fatalf("expected %v as fetched account address, got %v",
			accountA.Address, fetchedAccount.Address)
	}

	if fetchedAccount.UUID != accountA.UUID {
		t.Fatalf("expected %v as fetched account id, got %v",
			accountA.UUID, fetchedAccount.UUID)
	}

	// Ensure accounts cannot be updated.
	err = accountB.Update(db)
	if err == nil {
		t.Fatal("expected not supported error")
	}

	// Delete all accounts.
	err = accountA.Delete(db)
	if err != nil {
		t.Fatalf("delete accountA error: %v ", err)
	}

	err = accountB.Delete(db)
	if err != nil {
		t.Fatalf("delete accountB error: %v ", err)
	}

	// Ensure the account have been deleted.
	_, err = FetchAccount(db, []byte(accountA.UUID))
	if err == nil {
		t.Fatal("expected no account found error")
	}
}
