package pool

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

var dbUpgradeTests = [...]struct {
	verify   func(*testing.T, *bolt.DB)
	filename string // in testdata directory
}{
	// No upgrade test for V1, it is a backwards-compatible upgrade
	{verifyV2Upgrade, "v1.db.gz"},
	{verifyV3Upgrade, "v2.db.gz"},
	{verifyV4Upgrade, "v2.db.gz"},
}

func TestUpgrades(t *testing.T) {
	t.Parallel()

	d, err := ioutil.TempDir("", "dcrpool_test_upgrades")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("group", func(t *testing.T) {
		for i, test := range dbUpgradeTests {
			test := test
			name := fmt.Sprintf("test%d", i)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				testFile, err := os.Open(filepath.Join("testdata", test.filename))
				if err != nil {
					t.Fatal(err)
				}
				defer testFile.Close()
				r, err := gzip.NewReader(testFile)
				if err != nil {
					t.Fatal(err)
				}
				dbPath := filepath.Join(d, name+".db")
				fi, err := os.Create(dbPath)
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.Copy(fi, r)
				fi.Close()
				if err != nil {
					t.Fatal(err)
				}
				db, err := openDB(dbPath)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				err = upgradeDB(db)
				if err != nil {
					t.Fatalf("Upgrade failed: %v", err)
				}
				test.verify(t, db)
			})
		}
	})

	os.RemoveAll(d)
}

func verifyV2Upgrade(t *testing.T, db *bolt.DB) {
	funcName := "verifyV2Upgrade"
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("%s: bucket %s not found", funcName,
				string(poolBkt))
			return dbError(ErrBucketNotFound, desc)
		}

		sbkt := pbkt.Bucket(shareBkt)
		if sbkt == nil {
			desc := fmt.Sprintf("%s: bucket %s not found", funcName,
				string(shareBkt))
			return dbError(ErrBucketNotFound, desc)
		}

		c := sbkt.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var share Share
			err := json.Unmarshal(v, &share)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to unmarshal share: %v",
					funcName, err)
				return dbError(ErrParse, desc)
			}

			if string(k) != share.UUID {
				desc := fmt.Sprintf("expected share id (%s) to be the same as "+
					"its key (%x)", share.UUID, k)
				return dbError(ErrID, desc)
			}
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

func verifyV3Upgrade(t *testing.T, db *bolt.DB) {
	funcName := "verifyV3Upgrade"
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("%s: bucket %s not found",
				funcName, string(poolBkt))
			return dbError(ErrBucketNotFound, desc)
		}

		sbkt := pbkt.Bucket(paymentBkt)
		if sbkt == nil {
			desc := fmt.Sprintf("%s: bucket %s not found",
				funcName, string(paymentBkt))
			return dbError(ErrBucketNotFound, desc)
		}

		c := sbkt.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
					funcName, err)
				return dbError(ErrParse, desc)
			}

			id, err := paymentID(payment.Height, payment.CreatedOn, payment.Account)
			if err != nil {
				return err
			}
			if !bytes.Equal(k, id) {
				desc := fmt.Sprintf("%s: expected payment id (%x) to be "+
					"the same as its key (%x)", funcName, id, k)
				return dbError(ErrID, desc)

			}

			if payment.Source == nil {
				desc := fmt.Sprintf("%s: expected a non-nil "+
					"payment source: %v", funcName, err)
				return poolError(ErrPaymentSource, desc)
			}
		}

		abkt := pbkt.Bucket(paymentArchiveBkt)
		if sbkt == nil {
			desc := fmt.Sprintf("%s: bucket %s not found",
				funcName, string(paymentArchiveBkt))
			return dbError(ErrBucketNotFound, desc)
		}

		c = abkt.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				return err
			}

			id, err := paymentID(payment.Height, payment.CreatedOn, payment.Account)
			if err != nil {
				return err
			}
			if !bytes.Equal(k, id) {
				desc := fmt.Sprintf("%s: expected archived payment id "+
					"(%x) to be the same as its key (%x)", funcName, id, k)
				return poolError(ErrID, desc)
			}

			if payment.Source == nil {
				desc := fmt.Sprintf("%s: expected a non-nil payment source",
					funcName)
				return poolError(ErrPaymentSource, desc)
			}
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

func verifyV4Upgrade(t *testing.T, db *bolt.DB) {
	funcName := "verifyV4Upgrade"
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
			return dbError(ErrBucketNotFound, desc)
		}

		v := pbkt.Get([]byte("txfeereserve"))
		if v != nil {
			desc := fmt.Sprintf("%s: unexpected value found for "+
				"txfeereserve", funcName)
			return poolError(ErrValueFound, desc)
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}
