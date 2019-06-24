// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"math/big"
	"os"
	"testing"
	"time"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
)

var (
	// TestDB represents the testing database.
	testDB = "testdb"
	// Account X address.
	xAddr = "SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc"

	// Account X id.
	xID = ""

	// Account Y address.
	yAddr = "Ssp7J7TUmi5iPhoQnWYNGQbeGhu6V3otJcS"

	// Account Y id.
	yID = ""

	// Pool fee address.
	poolFeeAddrs, _ = dcrutil.DecodeAddress("SsnbEmxCVXskgTHXvf3rEa17NA39qQuGHwQ")
)

// setupDB initializes the pool database.
func setupDB() (*bolt.DB, error) {
	os.Remove(testDB)

	db, err := openDB(testDB)
	if err != nil {
		return nil, err
	}

	err = createBuckets(db)
	if err != nil {
		return nil, err
	}

	err = upgradeDB(db)
	if err != nil {
		return nil, err
	}

	xID, err = AccountID(xAddr)
	if err != nil {
		return nil, err
	}

	yID, err = AccountID(yAddr)
	if err != nil {
		return nil, err
	}

	err = createPersistedAccount(db, xAddr)
	if err != nil {
		return nil, err
	}

	err = createPersistedAccount(db, yAddr)
	if err != nil {
		return nil, err
	}

	return db, err
}

// teardownDB closes the connection to the db and deletes the db file.
func teardownDB(db *bolt.DB) error {
	err := db.Close()
	if err != nil {
		return err
	}

	err = os.Remove(testDB)
	if err != nil {
		return err
	}

	return nil
}

// createPersistedShare creates a share with the provided account, weight
// and created on time. The share is then persisted to the database.
func createPersistedShare(db *bolt.DB, account string, weight *big.Rat,
	createdOnNano int64) error {
	share := &Share{
		Account:   account,
		Weight:    weight,
		CreatedOn: createdOnNano,
	}

	return share.Create(db)
}

// createMultiplePersistedShares creates multiple shares per the count provided.
func createMultiplePersistedShares(db *bolt.DB, account string, weight *big.Rat,
	createdOnNano int64, count int) error {
	for idx := 0; idx < count; idx++ {
		err := createPersistedShare(db, account, weight, createdOnNano+int64(idx))
		if err != nil {
			return err
		}
	}

	return nil
}

func TestPPSEligibleShares(t *testing.T) {
	db, err := setupDB()
	if err != nil {
		t.Error(err)
	}

	td := func() {
		err = teardownDB(db)
		if err != nil {
			t.Error(err)
		}
	}

	defer td()

	now := time.Now()
	minNano := now.Add(-(time.Second * 60)).UnixNano()
	belowMinNano := now.Add(-(time.Second * 80)).UnixNano()
	maxNano := now.Add(-(time.Second * 30)).UnixNano()
	aboveMaxNano := now.Add(time.Second * 10).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)
	shareCount := 5
	expectedShareCount := 10

	err = createMultiplePersistedShares(db, xID, weight, belowMinNano, shareCount)
	if err != nil {
		t.Error(err)
	}

	err = createMultiplePersistedShares(db, xID, weight, minNano, shareCount)
	if err != nil {
		t.Error(err)
	}

	err = createMultiplePersistedShares(db, yID, weight, maxNano, shareCount)
	if err != nil {
		t.Error(err)
	}

	err = createMultiplePersistedShares(db, yID, weight, aboveMaxNano, shareCount)
	if err != nil {
		t.Error(err)
	}

	minNanoBytes := nanoToBigEndianBytes(minNano)
	nowNanoBytes := nanoToBigEndianBytes(now.UnixNano())
	shares, err := PPSEligibleShares(db, minNanoBytes, nowNanoBytes)
	if err != nil {
		t.Error(err)
	}

	if len(shares) != expectedShareCount {
		t.Errorf("Expected %v eligible PPS shares, got %v",
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

	if forAccX == 0 || forAccY == 0 {
		t.Errorf("Expected shares for account X and Y, "+
			"got %v (for x), %v (for y).", forAccX, forAccY)
	}

	if forAccX != forAccY {
		t.Errorf("Expected equal shares for account X and Y, "+
			"got %v (for x), %v (for y).", forAccX, forAccY)
	}

	if forAccX != shareCount && forAccY != shareCount {
		t.Errorf("Expected share counts of %v for account X and Y, "+
			"got %v (for x), %v (for y).", shareCount, forAccX, forAccY)
	}
}

func TestPPLNSEligibleShares(t *testing.T) {
	db, err := setupDB()
	if err != nil {
		t.Error(err)
	}

	td := func() {
		err = teardownDB(db)
		if err != nil {
			t.Error(err)
		}
	}

	defer td()

	now := time.Now()
	aboveMinNano := now.Add(-(time.Second * 20)).UnixNano()
	minNano := now.Add(-(time.Second * 30)).UnixNano()
	belowMinNano := now.Add(-(time.Second * 40)).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)
	shareCount := 10
	expectedShareCount := 10

	err = createMultiplePersistedShares(db, xID, weight, belowMinNano,
		shareCount)
	if err != nil {
		t.Error(err)
	}

	err = createMultiplePersistedShares(db, yID, weight, aboveMinNano,
		shareCount)
	if err != nil {
		t.Error(err)
	}

	minNanoBytes := nanoToBigEndianBytes(minNano)
	shares, err := PPLNSEligibleShares(db, minNanoBytes)
	if err != nil {
		t.Error(err)
	}

	if len(shares) != expectedShareCount {
		t.Errorf("Expected %v eligible PPLNS shares, got %v",
			expectedShareCount, len(shares))
	}
}

func TestCalculateDividend(t *testing.T) {
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
				t.Errorf("%s: error generated was %v, expected %v.",
					name, errCode.String(), expectedCode.String())
			}
		}

		for account, dividend := range test.output {
			if actual[account].Cmp(dividend) != 0 {
				t.Errorf("%s: account %v dividend was %v, "+
					"expected %v.", name, account, actual[account], dividend)
			}
		}
	}
}

func TestCalculatePoolTarget(t *testing.T) {
	set := []struct {
		hashRate   *big.Int
		targetTime *big.Int
		expected   string
	}{
		{
			new(big.Int).SetInt64(1.2E12),
			new(big.Int).SetInt64(15),
			"6434354813162443865075659925303014480581657380081282215060527505",
		},
		{
			new(big.Int).SetInt64(1.2E12),
			new(big.Int).SetInt64(10),
			"9652684091353612529418909805592420577743338497150222871859509577",
		},
	}

	for _, test := range set {
		target, _, err := calculatePoolTarget(&chaincfg.MainNetParams,
			test.hashRate, test.targetTime)
		if err != nil {
			t.Error(err)
		}

		expected, success := new(big.Int).SetString(test.expected, 10)
		if !success {
			t.Errorf("failed to parse %v as a big.Int", test.expected)
		}

		if target.Cmp(expected) != 0 {
			t.Errorf("for a hashrate of %v and a target time of %v the "+
				"expected target is %v, got %v", test.hashRate,
				test.targetTime, expected, target)
		}
	}
}

// Currently not using cursor delete's even though this test passing. An
// identical test in a PR for dcrwallet is failing, inconsistent
// behaviour.
func TestBoltDBCursorDeletion(t *testing.T) {
	db, err := setupDB()
	if err != nil {
		t.Error(err)
	}

	td := func() {
		err = teardownDB(db)
		if err != nil {
			t.Error(err)
		}
	}

	defer td()

	nowNano := time.Now().UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)
	shareCount := 10

	err = createMultiplePersistedShares(db, xID, weight, nowNano, shareCount)
	if err != nil {
		t.Error(err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchShareBucket(tx)
		if err != nil {
			return err
		}

		c := bkt.Cursor()
		iterations := 0
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			iterations++
		}

		if iterations != shareCount {
			t.Errorf("Expected to iterate over %v k/v pairs, but "+
				"iterated %v time(s)", shareCount, iterations)
		}

		// Test if the iterator can be used to delete each.
		iterations = 0
		c = bkt.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			err = c.Delete()
			if err != nil {
				return err
			}
			iterations++
		}
		if iterations != shareCount {
			t.Errorf("Expected to iterate over and delete %v k/v pairs, "+
				"but iterated %v time(s)", shareCount, iterations)
		}

		c = bkt.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			t.Errorf("Expected empty bucket, still has key %x", k)
		}

		return nil
	})
	if err != nil {
		t.Error(err)
	}
}
