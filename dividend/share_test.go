package dividend

import (
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"

	"dnldd/dcrpool/database"
)

var (
	// TestDB represents the testing database.
	testDB = "testdb"
	// Account X.
	accX = "x"
	// Account X address.
	xAddr = "SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc"
	// Account Y.
	accY = "y"
	// Account Y address.
	yAddr = "Ssp7J7TUmi5iPhoQnWYNGQbeGhu6V3otJcS"
	// Pool fee address.
	poolFeeAddrs, _ = dcrutil.DecodeAddress("SsnbEmxCVXskgTHXvf3rEa17NA39qQuGHwQ")
)

// setupDB initializes the pool database.
func setupDB() (*bolt.DB, error) {
	db, err := database.OpenDB(testDB)
	if err != nil {
		return nil, err
	}

	err = database.CreateBuckets(db)
	if err != nil {
		return nil, err
	}

	err = database.Upgrade(db)
	if err != nil {
		return nil, err
	}

	err = createPersistedAccount(db, accX, xAddr, accX)
	if err != nil {
		return nil, err
	}

	err = createPersistedAccount(db, accY, yAddr, accY)
	if err != nil {
		return nil, err
	}

	return db, err
}

// teardownDB closes the connection to the db and deletes the file.
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

func TestShareRangeScan(t *testing.T) {
	db, err := setupDB()
	if err != nil {
		t.Error(t)
	}

	now := time.Now()
	minNano := pastTime(&now, 0, 0, time.Duration(30), 0).UnixNano()
	belowMinNano := pastTime(&now, 0, 0, time.Duration(60), 0).UnixNano()
	maxNano := pastTime(&now, 0, 0, 0, time.Duration(30)).UnixNano()
	aboveMaxNano := futureTime(&now, 0, 0, time.Duration(30), 0).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)

	err = createMultiplePersistedShares(db, accX, weight, belowMinNano, 5)
	if err != nil {
		t.Error(t)
	}

	err = createMultiplePersistedShares(db, accX, weight, minNano, 5)
	if err != nil {
		t.Error(t)
	}

	err = createMultiplePersistedShares(db, accY, weight, maxNano, 5)
	if err != nil {
		t.Error(t)
	}

	err = createMultiplePersistedShares(db, accY, weight, aboveMaxNano, 5)
	if err != nil {
		t.Error(t)
	}

	minNanoBytes := NanoToBigEndianBytes(minNano)
	nowNanoBytes := NanoToBigEndianBytes(now.UnixNano())
	shares, err := FetchEligibleShares(db, minNanoBytes, nowNanoBytes)
	if err != nil {
		t.Error(t)
	}

	if len(shares) != 10 {
		t.Errorf("Expected %v eligible shares, got %v", 10, len(shares))
	}

	forAccX := 0
	forAccY := 0
	for _, share := range shares {
		if share.Account == accX {
			forAccX++
		}

		if share.Account == accY {
			forAccY++
		}
	}

	if forAccX != forAccY && (forAccX != 5 || forAccY != 5) {
		t.Errorf("Expected %v for account one, got %v. "+
			"Expected %v for account two, got %v.",
			5, forAccX, 5, forAccY)
	}

	err = teardownDB(db)
	if err != nil {
		t.Error(t)
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
			err:    ErrDivideByZero(),
		},
	}

	for name, test := range set {
		actual, err := CalculateSharePercentages(test.input)

		if err != test.err {
			errValue := ""
			expectedValue := ""

			if err != nil {
				errValue = err.Error()
			}

			if test.err != nil {
				expectedValue = test.err.Error()
			}

			if errValue != expectedValue {
				t.Errorf("(%s): error generated was (%v), expected (%v).",
					name, errValue, expectedValue)
			}
		}

		for account, dividend := range test.output {
			if actual[account].Cmp(dividend) != 0 {
				t.Errorf("(%s): account (%v) dividend was (%v), "+
					"expected (%v).", name, account, actual[account],
					dividend)
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
			"6432893846517566412420610278260439325181665814757809113303199112",
		},
		{
			new(big.Int).SetInt64(1.2E12),
			new(big.Int).SetInt64(10),
			"9649340769776349618630915417390658987772498722136713669954798668",
		},
	}

	for _, test := range set {
		target, err := CalculatePoolTarget(&chaincfg.SimNetParams,
			test.hashRate, test.targetTime)
		if err != nil {
			t.Error(err)
		}

		expected, success := new(big.Int).SetString(test.expected, 10)
		if !success {
			t.Errorf("Failed to parse (%v) as a big.Int", test.expected)
		}

		if target.Cmp(expected) != 0 {
			t.Errorf("For a hashrate of (%v) and a target time of (%v) the "+
				"expected target is (%v), got (%v).", test.hashRate,
				test.targetTime, expected, target)
		}
	}
}
