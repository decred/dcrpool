package pool

import (
	"fmt"
	"os"
	"testing"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg/v2"
)

func testInitDB(t *testing.T) {
	dbPath := "tdb"
	db, err := InitDB(dbPath, false)
	if err != nil {
		t.Fatal(err)
	}

	td := func() {
		err = teardownDB(db, dbPath)
		if err != nil {
			t.Fatalf("teardown error: %v", err)
		}
	}
	defer td()

	// Ensure the db bucket have been created.
	err = db.View(func(tx *bolt.Tx) error {
		var err error
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			return fmt.Errorf("poolBkt does not exist")
		}
		_, err = pbkt.CreateBucket(accountBkt)
		if err == nil {
			return fmt.Errorf("expected accountBkt to exist already")
		}
		_, err = pbkt.CreateBucket(shareBkt)
		if err == nil {
			return fmt.Errorf("expected shareBkt to exist already")
		}
		_, err = pbkt.CreateBucket(workBkt)
		if err == nil {
			return fmt.Errorf("expected workBkt to exist already")
		}
		_, err = pbkt.CreateBucket(jobBkt)
		if err == nil {
			return fmt.Errorf("expected jobBkt to exist already")
		}
		_, err = pbkt.CreateBucket(paymentBkt)
		if err == nil {
			return fmt.Errorf("expected paymentBkt to exist already")
		}
		_, err = pbkt.CreateBucket(paymentArchiveBkt)
		if err == nil {
			return fmt.Errorf("expected paymentArchiveBkt to exist already")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func testDatabase(t *testing.T, db *bolt.DB) {
	// Persist some accounts.
	accountA, err := persistAccount(db, "Ssj6Sd54j11JM8qpenCwfwnKD73dsjm68ru",
		chaincfg.SimNetParams())
	if err != nil {
		t.Fatal(err)
	}

	_, err = persistAccount(db, "SssPc1UNr8czcP3W9hfAgpmLRa3zJPDhfSy",
		chaincfg.SimNetParams())
	if err != nil {
		t.Fatal(err)
	}

	// delete the created account.
	err = deleteEntry(db, accountBkt, []byte(accountA.UUID))
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Ensure the accountA has been removed.
	_, err = FetchAccount(db, []byte(accountA.UUID))
	if err == nil {
		t.Fatalf("expected no value found error")
	}

	// purge the db.
	err = purge(db)
	if err != nil {
		t.Fatalf("backup error: %v", err)
	}

	// Ensure the account X and Y have been removed.
	_, err = FetchAccount(db, []byte(xID))
	if err == nil {
		t.Fatalf("expected no value found error for %s", xID)
	}
	_, err = FetchAccount(db, []byte(yID))
	if err == nil {
		t.Fatalf("expected no value found error for %s", yID)
	}

	// Create a database backup.
	backupFile := "backup.db"
	defer func() {
		os.Remove(backupFile)
	}()

	err = backup(db, backupFile)
	if err != nil {
		t.Fatalf("backup error: %v", err)
	}

	// Recreate account X and Y.
	_, err = persistAccount(db, xAddr, chaincfg.SimNetParams())
	if err != nil {
		t.Fatal(err)
	}
	_, err = persistAccount(db, yAddr, chaincfg.SimNetParams())
	if err != nil {
		t.Fatal(err)
	}
}
