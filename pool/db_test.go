// Copyright (c) 2021-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	errs "github.com/decred/dcrpool/errors"
)

func Test_BoltDB_FetchBucketHelpers(t *testing.T) {
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
	if !errors.Is(err, errs.StorageNotFound) {
		t.Fatalf("expected bucket not found error, got %v", err)
	}

	err = deleteEntry(db, paymentBkt, "k")
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
			binary.LittleEndian.PutUint32(vbytes, BoltDBVersion)
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
	if !errors.Is(err, errs.StorageNotFound) {
		t.Fatalf("expected bucket not found error, got %v", err)
	}
}

func Test_BoltDB_HttpBackup(t *testing.T) {
	dbPath := "tdb"
	os.Remove(dbPath)
	db, err := InitBoltDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		err = teardownBoltDB(db, dbPath)
		if err != nil {
			t.Fatalf("teardown error: %v", err)
		}
	}()

	// Capture the HTTP response written by the backup func.
	rr := httptest.NewRecorder()
	err = db.httpBackup(rr)
	if err != nil {
		t.Fatal(err)
	}

	// Check the HTTP status is OK.
	if status := rr.Code; status != http.StatusOK {
		t.Fatalf("wrong HTTP status code: expected %v, got %v",
			http.StatusOK, status)
	}

	// Check HTTP headers.
	header := "Content-Type"
	expected := "application/octet-stream"
	if actual := rr.Header().Get(header); actual != expected {
		t.Errorf("wrong %s header: expected %s, got %s",
			header, expected, actual)
	}

	header = "Content-Disposition"
	expected = `attachment; filename="backup.db"`
	if actual := rr.Header().Get(header); actual != expected {
		t.Errorf("wrong %s header: expected %s, got %s",
			header, expected, actual)
	}

	header = "Content-Length"
	cLength, err := strconv.Atoi(rr.Header().Get(header))
	if err != nil {
		t.Fatalf("could not convert %s to integer: %v", header, err)
	}

	if cLength <= 0 {
		t.Fatalf("expected a %s greater than zero, got %d", header, cLength)
	}

	// Check reported length matches actual.
	res := rr.Result()
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("could not read http response body: %v", err)
	}

	if len(body) != cLength {
		t.Fatalf("expected reported content-length to match actual body length. %v != %v",
			cLength, len(body))
	}
}

func Test_BoltDB_InitDB(t *testing.T) {
	dbPath := "tdb"
	os.Remove(dbPath)
	db, err := InitBoltDB(dbPath)
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
		_, err = pbkt.CreateBucket(hashDataBkt)
		if err == nil {
			return fmt.Errorf("expected hashDataBkt to exist already")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("db create error: %v", err)
	}
}

func testCSRFSecret(t *testing.T) {
	// Expect an error if no value set.
	_, err := db.fetchCSRFSecret()
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected value not found error, got: %v", err)
	}

	// Set and retrieve a value. The csrf secret is randomly generated bytes
	// which dont necessarily lie within the valid range of UTF-8, so add some
	// extra non UTF-8 bytes \xbd \x00.
	csrfSecret := []byte("really super secret string \xbd \x00")
	err = db.persistCSRFSecret(csrfSecret)
	if err != nil {
		t.Fatal(err)
	}

	fetched, err := db.fetchCSRFSecret()
	if err != nil {
		t.Fatal(err)
	}

	// Ensure retrieved value matches persisted value.
	if !bytes.Equal(fetched, csrfSecret) {
		t.Fatalf("expected csrf secret %q but got %q", csrfSecret, fetched)
	}

	// Ensure value can be updated.
	csrfSecret = []byte("another completely secret string")
	err = db.persistCSRFSecret(csrfSecret)
	if err != nil {
		t.Fatal(err)
	}

	fetched, err = db.fetchCSRFSecret()
	if err != nil {
		t.Fatal(err)
	}

	// Ensure retrieved value matches persisted value.
	if !bytes.Equal(fetched, csrfSecret) {
		t.Fatalf("expected csrf secret %q but got %q", csrfSecret, fetched)
	}
}

func testLastPaymentCreatedOn(t *testing.T) {
	// Expect an error if no value set.
	_, err := db.loadLastPaymentCreatedOn()
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected value not found error, got: %v", err)
	}

	// Set some values.
	lastPaymentCreatedOn := time.Now().UnixNano()
	err = db.persistLastPaymentCreatedOn(lastPaymentCreatedOn)
	if err != nil {
		t.Fatalf("unable to persist last payment created on: %v", err)
	}

	// Ensure values can be retrieved.
	paymentCreatedOn, err := db.loadLastPaymentCreatedOn()
	if err != nil {
		t.Fatalf("unable to load last payment created on: %v", err)
	}
	if lastPaymentCreatedOn != paymentCreatedOn {
		t.Fatalf("expected last payment created on to be %d, got %d",
			lastPaymentCreatedOn, paymentCreatedOn)
	}
}

func testPoolMode(t *testing.T) {
	// Expect an error if no value set.
	_, err := db.fetchPoolMode()
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected value not found error, got: %v", err)
	}

	// Set pool mode to 1.
	err = db.persistPoolMode(1)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure retrieved value matches persisted value.
	mode, err := db.fetchPoolMode()
	if err != nil {
		t.Fatal(err)
	}
	if mode != 1 {
		t.Fatalf("Expected pool mode to be solo")
	}

	// Set pool mode to 0.
	err = db.persistPoolMode(0)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure retrieved value matches persisted value.
	mode, err = db.fetchPoolMode()
	if err != nil {
		t.Fatal(err)
	}
	if mode != 0 {
		t.Fatalf("Expected pool mode to be 0")
	}
}

func testLastPaymentInfo(t *testing.T) {
	// Expect an error if no value set.
	_, _, err := db.loadLastPaymentInfo()
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected value not found error, got: %v", err)
	}

	// Set some values.
	lastPaymentHeight := uint32(1)
	lastPaymentPaidOn := time.Now().UnixNano()
	err = db.persistLastPaymentInfo(lastPaymentHeight, lastPaymentPaidOn)
	if err != nil {
		t.Fatalf("unable to persist last payment info: %v", err)
	}

	// Ensure values can be retrieved.
	paymentHeight, paymentPaidOn, err := db.loadLastPaymentInfo()
	if err != nil {
		t.Fatalf("unable to load last payment info: %v", err)
	}

	if lastPaymentHeight != paymentHeight {
		t.Fatalf("expected last payment height to be %d, got %d",
			paymentHeight, paymentHeight)
	}

	if lastPaymentPaidOn != paymentPaidOn {
		t.Fatalf("expected last payment paid on to be %d, got %d",
			lastPaymentPaidOn, paymentPaidOn)
	}
}
