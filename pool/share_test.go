// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrpool/pool/errors"
	bolt "go.etcd.io/bbolt"
)

// persistShare creates a persisted share with the provided account and share
// weight.
func persistShare(db *bolt.DB, account string, weight *big.Rat,
	createdOnNano int64) error {
	share := &Share{
		UUID:    string(shareID(account, createdOnNano)),
		Account: account,
		Weight:  weight,
	}

	err := share.Create(db)
	if err != nil {
		return fmt.Errorf("unable to persist share: %v", err)
	}

	return nil
}

func testShares(t *testing.T, db *bolt.DB) {
	shareACreatedOn := time.Now().Add(-(time.Second * 10)).UnixNano()
	shareBCreatedOn := time.Now().Add(-(time.Second * 20)).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)
	err := persistShare(db, xID, weight, shareACreatedOn) // Share A
	if err != nil {
		t.Fatal(err)
	}

	err = persistShare(db, yID, weight, shareBCreatedOn) // Share B
	if err != nil {
		t.Fatal(err)
	}

	// Fetch share A and B.
	shareA, err := fetchShare(db, shareID(xID, shareACreatedOn))
	if err != nil {
		t.Fatalf("unexpected error fetching share A: %v", err)
	}
	shareB, err := fetchShare(db, shareID(yID, shareBCreatedOn))
	if err != nil {
		t.Fatalf("unexpected error fetching share B: %v", err)
	}

	// Ensure shares cannot be updated.
	shareA.Weight = new(big.Rat).SetFloat64(100.0)
	err = shareA.Update(db)
	if err == nil {
		t.Fatal("expected an unsupported functionality error")
	}

	// Ensure shares cannot be deleted.
	err = shareB.Delete(db)
	if err == nil {
		t.Fatal("expected an unsupported functionality error")
	}

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}
}

func testSharePercentages(t *testing.T) {
	set := map[string]struct {
		input  []*Share
		output map[string]*big.Rat
		err    error
	}{
		"equal shares": {
			input: []*Share{
				NewShare("a", new(big.Rat).SetInt64(5)),
				NewShare("b", new(big.Rat).SetInt64(5)),
				NewShare("c", new(big.Rat).SetInt64(5)),
				NewShare("d", new(big.Rat).SetInt64(5)),
				NewShare("e", new(big.Rat).SetInt64(5)),
			},
			output: map[string]*big.Rat{
				"a": new(big.Rat).SetFrac64(5, 25),
				"b": new(big.Rat).SetFrac64(5, 25),
				"c": new(big.Rat).SetFrac64(5, 25),
				"d": new(big.Rat).SetFrac64(5, 25),
				"e": new(big.Rat).SetFrac64(5, 25),
			},
			err: nil,
		},
		"inequal shares": {
			input: []*Share{
				NewShare("a", new(big.Rat).SetInt64(5)),
				NewShare("b", new(big.Rat).SetInt64(10)),
				NewShare("c", new(big.Rat).SetInt64(15)),
				NewShare("d", new(big.Rat).SetInt64(20.0)),
				NewShare("e", new(big.Rat).SetInt64(25.0)),
			},
			output: map[string]*big.Rat{
				"a": new(big.Rat).SetFrac64(5, 75),
				"b": new(big.Rat).SetFrac64(10, 75),
				"c": new(big.Rat).SetFrac64(15, 75),
				"d": new(big.Rat).SetFrac64(20, 75),
				"e": new(big.Rat).SetFrac64(25, 75),
			},
			err: nil,
		},
		"zero shares": {
			input: []*Share{
				NewShare("a", new(big.Rat)),
				NewShare("b", new(big.Rat)),
				NewShare("c", new(big.Rat)),
				NewShare("d", new(big.Rat)),
				NewShare("e", new(big.Rat)),
			},
			output: nil,
			err:    errors.MakeError(errors.ErrDivideByZero, "division by zero", nil),
		},
	}

	for name, test := range set {
		actual, err := sharePercentages(test.input)
		if err != test.err {
			var errCode errors.ErrorCode
			var expectedCode errors.ErrorCode

			if err != nil {
				e, ok := err.(errors.Error)
				if ok {
					errCode = e.ErrorCode
				}
			}

			if test.err != nil {
				e, ok := test.err.(errors.Error)
				if ok {
					expectedCode = e.ErrorCode
				}
			}

			if errCode.String() != expectedCode.String() {
				t.Fatalf("%s: error generated was %v, expected %v.",
					name, errCode.String(), expectedCode.String())
			}
		}

		for account, dividend := range test.output {
			if actual[account].Cmp(dividend) != 0 {
				t.Fatalf("%s: account %v dividend was %v, "+
					"expected %v.", name, account, actual[account], dividend)
			}
		}
	}
}

func testCalculatePoolTarget(t *testing.T) {
	set := []struct {
		hashRate   *big.Int
		targetTime *big.Int
		expected   string
	}{
		{
			new(big.Int).SetInt64(1.2e12),
			new(big.Int).SetInt64(15),
			"942318434548471642444425333729556541774658078333663444331523307356028928/146484375",
		},
		{
			new(big.Int).SetInt64(1.2e12),
			new(big.Int).SetInt64(10),
			"471159217274235821222212666864778270887329039166831722165761653678014464/48828125",
		},
	}

	for _, test := range set {
		target, _, err := calculatePoolTarget(chaincfg.MainNetParams(),
			test.hashRate, test.targetTime)
		if err != nil {
			t.Fatal(err)
		}

		expected, success := new(big.Rat).SetString(test.expected)
		if !success {
			t.Fatalf("failed to parse %v as a big.Int", test.expected)
		}

		if target.Cmp(expected) != 0 {
			t.Fatalf("for a hashrate of %v and a target time of %v the "+
				"expected target is %v, got %v", test.hashRate,
				test.targetTime, expected, target)
		}
	}
}
