package pool

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/decred/dcrd/chaincfg/v2"
	bolt "go.etcd.io/bbolt"
)

func initBlankDB(dbFile string) (*bolt.DB, error) {
	os.Remove(dbFile)
	db, err := openDB(dbFile)
	if err != nil {
		return nil, MakeError(ErrDBOpen, "unable to open db file", err)
	}

	return db, nil
}

func testFetchBucketHelpers(t *testing.T) {
	dbPath := "tdb"
	db, err := initBlankDB(dbPath)
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

	expectedNotFoundErr := fmt.Errorf("expected main bucket not found error")

	pmtMgr := &PaymentMgr{}

	// Ensure payment manager helpers return an error when the
	// pool bucket cannot be found.
	err = db.View(func(tx *bolt.Tx) error {
		err = pmtMgr.loadLastPaymentCreatedOn(tx)
		if err == nil {
			return expectedNotFoundErr
		}
		err = pmtMgr.loadLastPaymentHeight(tx)
		if err == nil {
			return expectedNotFoundErr
		}
		err = pmtMgr.loadLastPaymentPaidOn(tx)
		if err == nil {
			return expectedNotFoundErr
		}
		err = pmtMgr.persistLastPaymentCreatedOn(tx)
		if err == nil {
			return expectedNotFoundErr
		}
		err = pmtMgr.persistLastPaymentHeight(tx)
		if err == nil {
			return expectedNotFoundErr
		}
		err = pmtMgr.persistLastPaymentPaidOn(tx)
		if err == nil {
			return expectedNotFoundErr
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Ensure fetch bucket helpers return an error when the
	// pool bucket cannot be found.
	err = db.View(func(tx *bolt.Tx) error {
		_, err := fetchWorkBucket(tx)
		if err == nil {
			return expectedNotFoundErr
		}
		_, err = fetchAccountBucket(tx)
		if err == nil {
			return expectedNotFoundErr
		}
		_, err = fetchWorkBucket(tx)
		if err == nil {
			return expectedNotFoundErr
		}
		_, err = fetchJobBucket(tx)
		if err == nil {
			return expectedNotFoundErr
		}
		_, err = fetchPaymentBucket(tx)
		if err == nil {
			return expectedNotFoundErr
		}
		_, err = fetchPaymentArchiveBucket(tx)
		if err == nil {
			return expectedNotFoundErr
		}
		_, err = fetchShareBucket(tx)
		if err == nil {
			return expectedNotFoundErr
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = deleteEntry(db, paymentBkt, []byte("k"))
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
	err = db.Update(func(tx *bolt.Tx) error {
		var err error
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			pbkt, err = tx.CreateBucketIfNotExists(poolBkt)
			if err != nil {
				desc := fmt.Sprintf("failed to create %s bucket", string(poolBkt))
				return MakeError(ErrBucketCreate, desc, err)
			}
			vbytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(vbytes, uint32(DBVersion))
			err = pbkt.Put(versionK, vbytes)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("db update error: %v", err)
	}

	expectedNestedNotFoundErr := fmt.Errorf("expected nested bucket not found error")

	// Ensure fetch bucket helpers return an error if the
	// required nested bucket cannot be found.
	err = db.View(func(tx *bolt.Tx) error {
		_, err := fetchWorkBucket(tx)
		if err == nil {
			return expectedNestedNotFoundErr
		}
		_, err = fetchAccountBucket(tx)
		if err == nil {
			return expectedNestedNotFoundErr
		}
		_, err = fetchWorkBucket(tx)
		if err == nil {
			return expectedNestedNotFoundErr
		}
		_, err = fetchJobBucket(tx)
		if err == nil {
			return expectedNestedNotFoundErr
		}
		_, err = fetchPaymentBucket(tx)
		if err == nil {
			return expectedNestedNotFoundErr
		}
		_, err = fetchPaymentArchiveBucket(tx)
		if err == nil {
			return expectedNestedNotFoundErr
		}
		_, err = fetchShareBucket(tx)
		if err == nil {
			return expectedNestedNotFoundErr
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

}

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

	// Ensure the db buckets have been created.
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
		t.Fatalf("db create error: %v", err)
	}

	// Persist the pool mode.
	err = db.Update(func(tx *bolt.Tx) error {
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

	err = db.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Trigger a pool mode switch.
	db, err = InitDB(dbPath, true)
	if err != nil {
		t.Fatal(err)
	}

	// Delete the database backup.
	backup := filepath.Join(filepath.Dir(db.Path()), backupFile)
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		t.Fatalf("backup (%s) does not exist", backup)
	}
	err = os.Remove(backup)
	if err != nil {
		t.Fatalf("backup deletion error: %v", err)
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
