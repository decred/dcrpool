// Copyright (c) 2021-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

var boltDBUpgradeTests = [...]struct {
	verify   func(*testing.T, *BoltDB)
	filename string // in testdata directory
}{
	// No upgrade test for V1, it is a backwards-compatible upgrade
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
