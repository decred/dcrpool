// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/v2"
	bolt "go.etcd.io/bbolt"
)

// persistShare creates a persisted share with the provided account and share
// weight.
func persistShare(db *bolt.DB, account string, weight *big.Rat,
	createdOnNano int64) error {
	share := &Share{
		UUID:    shareID(account, createdOnNano),
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
	now := time.Now()
	minimumTime := now.Add(-(time.Second * 60)).UnixNano()
	maximumTime := now.UnixNano()
	belowMinimumTime := now.Add(-(time.Second * 80)).UnixNano()
	aboveMaximumTime := now.Add(time.Second * 10).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)

	shareCount := 1
	expectedShareCount := 2

	// Create a share below the PPS time range for account x.
	err := persistShare(db, xID, weight, belowMinimumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share at minimum of the PPS time range for account x.
	err = persistShare(db, xID, weight, minimumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share at maximum of the PPS time range for account y.
	err = persistShare(db, yID, weight, maximumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share above maximum of the PPS time range for account y.
	err = persistShare(db, yID, weight, aboveMaximumTime)
	if err != nil {
		t.Fatal(err)
	}

	minimumTimeBytes := nanoToBigEndianBytes(minimumTime)
	maximumTimeBytes := nanoToBigEndianBytes(maximumTime)

	// Fetch eligible shares using the minimum and maximum time range.
	shares, err := PPSEligibleShares(db, minimumTimeBytes, maximumTimeBytes)
	if err != nil {
		t.Error(err)
	}

	// Ensure the returned share count is as expected.
	if len(shares) != expectedShareCount {
		t.Errorf("PPS error: expected %v eligible PPS shares, got %v",
			expectedShareCount, len(shares))
	}

	forAccX := 0
	forAccY := 0
	for _, share := range shares {
		if share.Account == xID {
			forAccX++
		}

		if share.Account == yID {
			forAccY++
		}
	}

	// Ensure account x and account y both have shares returned.
	if forAccX == 0 || forAccY == 0 {
		t.Fatalf("PPS error: expected shares for account X and Y, "+
			"got %v (for x), %v (for y).", forAccX, forAccY)
	}

	// Ensure account x and account y have equal number of shares.
	if forAccX != forAccY {
		t.Fatalf("PPS error: expected equal shares for account X and Y, "+
			"got %v (for x), %v (for y).", forAccX, forAccY)
	}

	// Ensure account x and account y have shares equal to the share count.
	if forAccX != shareCount || forAccY != shareCount {
		t.Fatalf("PPS error: expected share counts of %v for account X and Y, "+
			"got %v (for x), %v (for y).", shareCount, forAccX, forAccY)
	}

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Create a share below the minimum exclusive PPLNS time for account x.
	err = persistShare(db, xID, weight, belowMinimumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share below the minimum exxcuive PPLNS time for account y.
	err = persistShare(db, yID, weight, belowMinimumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share at minimum exclusive PPLNS time for account x.
	err = persistShare(db, xID, weight, minimumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share at minimum exclusive PPLNS time for account y.
	err = persistShare(db, yID, weight, minimumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share above minimum exclusive PPLNS time for account x.
	err = persistShare(db, xID, weight, maximumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share above minimum exclusive PPLNS time for account y.
	err = persistShare(db, yID, weight, aboveMaximumTime)
	if err != nil {
		t.Fatal(err)
	}

	shares, err = PPLNSEligibleShares(db, minimumTimeBytes)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure the returned number of shates is as expected.
	if len(shares) != expectedShareCount {
		t.Errorf("PPLNS error: expected %v eligible PPLNS shares, got %v",
			expectedShareCount, len(shares))
	}

	// Ensure account x and account y both have shares returned.
	if forAccX == 0 || forAccY == 0 {
		t.Fatalf("PPLNS error: expected shares for account X and Y, "+
			"got %v (for x), %v (for y).", forAccX, forAccY)
	}

	// Ensure account x and account y have equal number of shares.
	if forAccX != forAccY {
		t.Fatalf("PPLNS error: expected equal shares for account X and Y, "+
			"got %v (for x), %v (for y).", forAccX, forAccY)
	}

	// Ensure account x and account y have shares equal to the share count.
	if forAccX != shareCount || forAccY != shareCount {
		t.Fatalf("PPLNS error: expected share counts of %v for account X and Y, "+
			"got %v (for x), %v (for y).", shareCount, forAccX, forAccY)
	}

	// Ensure a share cannot be updated or deleted.
	share := shares[0]
	err = share.Update(db)
	if err == nil {
		t.Fatal("expected a not supported error")
	}
	err = share.Delete(db)
	if err == nil {
		t.Fatal("expected a not supported error")
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
			err:    MakeError(ErrDivideByZero, "division by zero", nil),
		},
	}

	for name, test := range set {
		actual, err := sharePercentages(test.input)
		if err != test.err {
			var errCode ErrorCode
			var expectedCode ErrorCode

			if err != nil {
				e, ok := err.(Error)
				if ok {
					errCode = e.ErrorCode
				}
			}

			if test.err != nil {
				e, ok := test.err.(Error)
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
