package dividend

import (
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/coreos/bbolt"

	"dnldd/dcrpool/database"
)

var (
	// TestDB reprsents the testing database.
	TestDB = "testdb"
)

// setupDB initializes the pool database.
func setupDB() (*bolt.DB, error) {
	db, err := database.OpenDB(TestDB)
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

	return db, err
}

// teardownDB closes the connection to the db and deletes the file.
func teardownDB(db *bolt.DB) error {
	err := db.Close()
	if err != nil {
		return err
	}

	err = os.Remove(TestDB)
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
	accOne := "dnldd"
	accTwo := "tmmy"
	err = createMultiplePersistedShares(db, accOne, weight, belowMinNano, 5)
	if err != nil {
		t.Error(t)
	}

	err = createMultiplePersistedShares(db, accOne, weight, minNano, 5)
	if err != nil {
		t.Error(t)
	}

	err = createMultiplePersistedShares(db, accTwo, weight, maxNano, 5)
	if err != nil {
		t.Error(t)
	}

	err = createMultiplePersistedShares(db, accTwo, weight, aboveMaxNano, 5)
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

	forAccOne := 0
	forAccTwo := 0
	for _, share := range shares {
		if share.Account == accOne {
			forAccOne++
		}

		if share.Account == accTwo {
			forAccTwo++
		}
	}

	if forAccOne != forAccTwo && (forAccOne != 5 || forAccTwo != 5) {
		t.Errorf("Expected %v for account one, got %v. "+
			"Expected %v for account two, got %v.",
			5, forAccOne, 5, forAccTwo)
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
			"1381437475988024283268563409791075016144953" +
				"2887812045339949604391304358105",
		},
		{
			new(big.Int).SetInt64(1.2E12),
			new(big.Int).SetInt64(10),
			"2072156213982036424902845114686612524217429" +
				"9331718068009924406586956537158",
		},
	}

	for _, test := range set {
		target := CalculatePoolTarget(test.hashRate, test.targetTime)
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
