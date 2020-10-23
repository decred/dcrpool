package pool

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestFetchBucketHelpers(t *testing.T) {
	// Create a new empty database.
	dbPath := "tdb"
	os.Remove(dbPath)
	db, err := openBoltDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		err = teardownBoltDB(db, dbPath)
		if err != nil {
			t.Fatalf("teardown error: %v", err)
		}
	}()

	expectedNotFoundErr := fmt.Errorf("expected main bucket not found error")

	// Ensure payment manager helpers return an error when the
	// pool bucket cannot be found.
	_, err = db.loadLastPaymentCreatedOn()
	if err == nil {
		t.Fatal(expectedNotFoundErr)
	}
	_, _, err = db.loadLastPaymentInfo()
	if err == nil {
		t.Fatal(expectedNotFoundErr)
	}
	err = db.persistLastPaymentCreatedOn(0)
	if err == nil {
		t.Fatal(expectedNotFoundErr)
	}
	err = db.persistLastPaymentInfo(0, 0)
	if err == nil {
		t.Fatal(expectedNotFoundErr)
	}

	// Ensure fetch bucket helper returns an error when the
	// pool bucket cannot be found.
	err = db.DB.View(func(tx *bolt.Tx) error {
		_, err := fetchBucket(tx, workBkt)
		return err
	})
	if !errors.Is(err, ErrBucketNotFound) {
		t.Fatalf("expected bucket not found error, got %v", err)
	}

	err = deleteEntry(db, paymentBkt, "k")
	if err == nil {
		t.Fatal(expectedNotFoundErr)
	}

	err = emptyBucket(db, paymentBkt)
	if err == nil {
		t.Fatal(expectedNotFoundErr)
	}

	err = purge(db)
	if err == nil {
		t.Fatal(expectedNotFoundErr)
	}

	// Create the pool database bucket.
	err = db.DB.Update(func(tx *bolt.Tx) error {
		var err error
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			pbkt, err = tx.CreateBucketIfNotExists(poolBkt)
			if err != nil {
				return fmt.Errorf("unable to create %s bucket: %v",
					string(poolBkt), err)

			}
			vbytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(vbytes, uint32(DBVersion))
			err = pbkt.Put(versionK, vbytes)
			if err != nil {
				return fmt.Errorf("unable to persist version: %v", err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("db update error: %v", err)
	}

	// Ensure fetch bucket helper returns an error if the
	// required nested bucket cannot be found.
	err = db.DB.View(func(tx *bolt.Tx) error {
		_, err := fetchBucket(tx, workBkt)
		return err
	})
	if !errors.Is(err, ErrBucketNotFound) {
		t.Fatalf("expected bucket not found error, got %v", err)
	}
}

func TestInitDB(t *testing.T) {
	dbPath := "tdb"
	os.Remove(dbPath)
	db, err := InitBoltDB(dbPath, false)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		err = teardownBoltDB(db, dbPath)
		if err != nil {
			t.Fatalf("teardown error: %v", err)
		}
	}()

	// Ensure the db buckets have been created.
	err = db.DB.View(func(tx *bolt.Tx) error {
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
		t.Fatalf("db create error: %v", err)
	}

	// Persist the pool mode.
	err = db.DB.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			return fmt.Errorf("poolBkt does not exist")
		}

		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, 0)
		return pbkt.Put(soloPool, b)
	})
	if err != nil {
		t.Fatalf("db update error: %v", err)
	}

	err = db.close()
	if err != nil {
		t.Fatal(err)
	}

	// Trigger a pool mode switch.
	db, err = InitBoltDB(dbPath, true)
	if err != nil {
		t.Fatal(err)
	}

	// Delete the database backup.
	backup := filepath.Join(filepath.Dir(db.DB.Path()), backupFile)
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		t.Fatalf("backup (%s) does not exist", backup)
	}
	err = os.Remove(backup)
	if err != nil {
		t.Fatalf("backup deletion error: %v", err)
	}
}

func testDatabase(t *testing.T) {
	// Persist some accounts.
	accountA := NewAccount("Ssj6Sd54j11JM8qpenCwfwnKD73dsjm68ru")
	err := db.persistAccount(accountA)
	if err != nil {
		t.Fatal(err)
	}

	accountB := NewAccount("SssPc1UNr8czcP3W9hfAgpmLRa3zJPDhfSy")
	err = db.persistAccount(accountB)
	if err != nil {
		t.Fatal(err)
	}

	// delete the created account.
	err = deleteEntry(db, accountBkt, accountA.UUID)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Ensure the accountA has been removed.
	_, err = db.fetchAccount(accountA.UUID)
	if !errors.Is(err, ErrValueNotFound) {
		t.Fatalf("expected no value found error: %v", err)
	}

	// purge the db.
	err = purge(db)
	if err != nil {
		t.Fatalf("backup error: %v", err)
	}

	// Ensure the account X and Y have been removed.
	_, err = db.fetchAccount(xID)
	if err == nil {
		t.Fatalf("expected no value found error for %s", xID)
	}
	_, err = db.fetchAccount(yID)
	if err == nil {
		t.Fatalf("expected no value found error for %s", yID)
	}

	// Create a database backup.
	backupFile := "backup.db"
	defer func() {
		os.Remove(backupFile)
	}()

	err = db.backup(backupFile)
	if err != nil {
		t.Fatal(err)
	}
}

func testLastPaymentCreatedOn(t *testing.T) {
	// Expect an error if no value set.
	_, err := db.loadLastPaymentCreatedOn()
	if !errors.Is(err, ErrValueNotFound) {
		t.Fatalf("[loadLastPaymentCreatedOn] expected value not found error, got: %v", err)
	}

	// Set some values.
	lastPaymentCreatedOn := time.Now().UnixNano()
	err = db.persistLastPaymentCreatedOn(lastPaymentCreatedOn)
	if err != nil {
		t.Fatalf("[persistLastPaymentCreatedOn] unable to persist last payment created on: %v", err)
	}

	// Ensure values can be retrieved.
	paymentCreatedOn, err := db.loadLastPaymentCreatedOn()
	if err != nil {
		t.Fatalf("[loadLastPaymentCreatedOn] unable to load last payment created on: %v", err)
	}
	if lastPaymentCreatedOn != paymentCreatedOn {
		t.Fatalf("[loadLastPaymentCreatedOn] expected last payment created on to be %d, got %d",
			lastPaymentCreatedOn, paymentCreatedOn)
	}
}

func testLastPaymentInfo(t *testing.T) {
	// Expect an error if no value set.
	_, _, err := db.loadLastPaymentInfo()
	if !errors.Is(err, ErrValueNotFound) {
		t.Fatalf("[loadLastPaymentInfo] expected value not found error, got: %v", err)
	}

	// Set some values.
	lastPaymentHeight := uint32(1)
	lastPaymentPaidOn := time.Now().UnixNano()
	err = db.persistLastPaymentInfo(lastPaymentHeight, lastPaymentPaidOn)
	if err != nil {
		t.Fatalf("[persistLastPaymentInfo] unable to persist last payment info: %v", err)
	}

	// Ensure values can be retrieved.
	paymentHeight, paymentPaidOn, err := db.loadLastPaymentInfo()
	if err != nil {
		t.Fatalf("[loadLastPaymentInfo] unable to load last payment info: %v", err)
	}

	if lastPaymentHeight != paymentHeight {
		t.Fatalf("[loadLastPaymentInfo] expected last payment height to be %d, got %d",
			paymentHeight, paymentHeight)
	}

	if lastPaymentPaidOn != paymentPaidOn {
		t.Fatalf("[loadLastPaymentInfo] expected last payment paid on to be %d, got %d",
			lastPaymentPaidOn, paymentPaidOn)
	}
}
