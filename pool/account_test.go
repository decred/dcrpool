package pool

import (
	"errors"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func persistAccount(db *bolt.DB, address string) (*Account, error) {
	acc := NewAccount(address)
	err := acc.Create(db)
	if err != nil {
		return nil, err
	}
	return acc, nil
}

func testAccount(t *testing.T, db *bolt.DB) {
	simnetAddrA := "Ssj6Sd54j11JM8qpenCwfwnKD73dsjm68ru"
	simnetAddrB := "SssPc1UNr8czcP3W9hfAgpmLRa3zJPDhfSy"

	// Create some valid accounts.
	accountA, err := persistAccount(db, simnetAddrA)
	if err != nil {
		t.Fatal(err)
	}
	accountB, err := persistAccount(db, simnetAddrB)
	if err != nil {
		t.Fatal(err)
	}

	// Creating the same account twice should fail.
	_, err = persistAccount(db, simnetAddrA)
	if !errors.Is(err, ErrValueFound) {
		t.Fatal("expected value found error")
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

	// Delete all accounts.
	err = accountA.Delete(db)
	if err != nil {
		t.Fatalf("delete accountA error: %v ", err)
	}

	err = accountB.Delete(db)
	if err != nil {
		t.Fatalf("delete accountB error: %v ", err)
	}

	// Ensure the accounts have both been deleted.
	_, err = FetchAccount(db, []byte(accountA.UUID))
	if !errors.Is(err, ErrValueNotFound) {
		t.Fatal("expected value not found error")
	}

	_, err = FetchAccount(db, []byte(accountB.UUID))
	if !errors.Is(err, ErrValueNotFound) {
		t.Fatal("expected value not found error")
	}
}
