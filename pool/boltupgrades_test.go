// Copyright (c) 2021-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

var boltDBUpgradeTests = [...]struct {
	verify   func(*testing.T, *BoltDB)
	filename string // in testdata directory
}{
	// No upgrade test for V1, it is a backwards-compatible upgrade
	{verifyV2Upgrade, "v1.db.gz"},
	{verifyV3Upgrade, "v2.db.gz"},
	{verifyV4Upgrade, "v2.db.gz"},
	{verifyV5Upgrade, "v4.db.gz"},
	{verifyV6Upgrade, "v5.db.gz"},
}

func TestBoltDBUpgrades(t *testing.T) {
	t.Parallel()

	d := t.TempDir()
	t.Run("group", func(t *testing.T) {
		t.Parallel()
		for i, test := range boltDBUpgradeTests {
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
				for {
					_, err = io.CopyN(fi, r, 1024)
					if err != nil {
						if errors.Is(err, io.EOF) {
							break
						}

						fi.Close()
						t.Fatal(err)
					}
				}
				fi.Close()
				db, err := openBoltDB(dbPath)
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
}

func verifyV2Upgrade(t *testing.T, db *BoltDB) {
	const funcName = "verifyV2Upgrade"
	err := db.DB.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			return fmt.Errorf("%s: bucket %s not found", funcName,
				string(poolBkt))
		}

		sbkt := pbkt.Bucket(shareBkt)
		if sbkt == nil {
			return fmt.Errorf("%s: bucket %s not found", funcName,
				string(shareBkt))
		}

		c := sbkt.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var share Share
			err := json.Unmarshal(v, &share)
			if err != nil {
				return fmt.Errorf("%s: unable to unmarshal share: %w",
					funcName, err)
			}

			if string(k) != share.UUID {
				return fmt.Errorf("%s: expected share id (%s) to be the same as "+
					"its key (%x)", funcName, share.UUID, k)
			}
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

func verifyV3Upgrade(t *testing.T, db *BoltDB) {
	const funcName = "verifyV3Upgrade"
	err := db.DB.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			return fmt.Errorf("%s: bucket %s not found",
				funcName, string(poolBkt))
		}

		sbkt := pbkt.Bucket(paymentBkt)
		if sbkt == nil {
			return fmt.Errorf("%s: bucket %s not found",
				funcName, string(paymentBkt))
		}

		c := sbkt.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				return fmt.Errorf("%s: unable to unmarshal payment: %w",
					funcName, err)
			}

			id := paymentID(payment.Height, payment.CreatedOn, payment.Account)
			if !bytes.Equal(k, []byte(id)) {
				return fmt.Errorf("%s: expected payment id (%s) to be "+
					"the same as its key (%x)", funcName, id, k)
			}

			if payment.Source == nil {
				return fmt.Errorf("%s: expected a non-nil payment source",
					funcName)
			}
		}

		abkt := pbkt.Bucket(paymentArchiveBkt)
		if sbkt == nil {
			return fmt.Errorf("%s: bucket %s not found",
				funcName, string(paymentArchiveBkt))
		}

		c = abkt.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				return fmt.Errorf("%s: unable to unmarshal payment: %w",
					funcName, err)
			}

			id := paymentID(payment.Height, payment.CreatedOn, payment.Account)
			if !bytes.Equal(k, []byte(id)) {
				return fmt.Errorf("%s: expected archived payment id "+
					"(%s) to be the same as its key (%x)", funcName, id, k)
			}

			if payment.Source == nil {
				return fmt.Errorf("%s: expected a non-nil payment source",
					funcName)
			}
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

func verifyV4Upgrade(t *testing.T, db *BoltDB) {
	const funcName = "verifyV4Upgrade"
	err := db.DB.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			return fmt.Errorf("%s: bucket %s not found", funcName,
				string(poolBkt))
		}

		v := pbkt.Get([]byte("txfeereserve"))
		if v != nil {
			return fmt.Errorf("%s: unexpected value found for "+
				"txfeereserve", funcName)
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

func verifyV5Upgrade(t *testing.T, db *BoltDB) {
	const funcName = "verifyV5Upgrade"
	err := db.DB.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			return fmt.Errorf("%s: bucket %s not found", funcName,
				string(poolBkt))
		}

		sbkt := pbkt.Bucket(shareBkt)
		if sbkt == nil {
			return fmt.Errorf("%s: bucket %s not found", funcName,
				string(shareBkt))
		}

		c := sbkt.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var share Share
			err := json.Unmarshal(v, &share)
			if err != nil {
				return fmt.Errorf("%s: unable to unmarshal share: %w",
					funcName, err)
			}

			if share.CreatedOn == 0 {
				return fmt.Errorf("%s: the created on "+
					"value for %s is not set", funcName, share.UUID)
			}

			createdOnHex := hex.EncodeToString(nanoToBigEndianBytes(
				share.CreatedOn))

			if string(k[:16]) != createdOnHex {
				return fmt.Errorf("%s: created on hex (%s) does not match "+
					"the extracted value from key (%x)", funcName,
					createdOnHex, k[:16])
			}
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

func verifyV6Upgrade(t *testing.T, db *BoltDB) {
	const funcName = "verifyV6Upgrade"
	err := db.DB.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			return fmt.Errorf("%s: bucket %s not found", funcName,
				string(poolBkt))
		}

		pmtbkt := pbkt.Bucket(paymentBkt)
		if pmtbkt == nil {
			return fmt.Errorf("%s: bucket %s not found", funcName,
				string(paymentBkt))
		}

		c := pmtbkt.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var pmt Payment
			err := json.Unmarshal(v, &pmt)
			if err != nil {
				return fmt.Errorf("%s: unable to unmarshal payment: %w",
					funcName, err)
			}

			if pmt.UUID == "" {
				return fmt.Errorf("%s: the payment UUID "+
					"value for %s is not set", funcName, pmt.UUID)
			}

			if strings.Compare(string(k), pmt.UUID) != 0 {
				return fmt.Errorf("%s: payment UUID (%s) does not match "+
					"the  payment key (%s)", funcName, pmt.UUID, string(k))
			}
		}

		abkt := pbkt.Bucket(paymentArchiveBkt)
		if abkt == nil {
			return fmt.Errorf("%s: bucket %s not found", funcName,
				string(paymentArchiveBkt))
		}

		c = abkt.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var pmt Payment
			err := json.Unmarshal(v, &pmt)
			if err != nil {
				return fmt.Errorf("%s: unable to unmarshal archived payment: %w",
					funcName, err)
			}

			if pmt.UUID == "" {
				return fmt.Errorf("%s: the payment UUID "+
					"value for %s is not set", funcName, pmt.UUID)
			}

			if strings.Compare(string(k), pmt.UUID) != 0 {
				return fmt.Errorf("%s: payment UUID (%s) does not match "+
					"the  payment key (%s)", funcName, pmt.UUID, string(k))
			}
		}

		return nil
	})
	if err != nil {
		t.Error(err)
	}
}
