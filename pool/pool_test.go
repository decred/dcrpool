package pool

import (
	"os"
	"testing"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	bolt "go.etcd.io/bbolt"
)

var (
	// testDB represents the database used in testing.
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
	poolFeeAddrs, _ = dcrutil.DecodeAddress(
		"SsnbEmxCVXskgTHXvf3rEa17NA39qQuGHwQ",
		chaincfg.SimNetParams())
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

	xID = AccountID(xAddr)
	yID = AccountID(yAddr)

	_, err = persistAccount(db, xAddr)
	if err != nil {
		return nil, err
	}
	_, err = persistAccount(db, yAddr)
	if err != nil {
		return nil, err
	}
	return db, err
}

// teardownDB closes the connection to the db and deletes the db file.
func teardownDB(db *bolt.DB, dbPath string) error {
	db.Close()
	return os.Remove(dbPath)
}

// TestPool runs all pool related tests.
func TestPool(t *testing.T) {
	db, err := setupDB()
	if err != nil {
		t.Fatalf("setup error: %v", err)
	}

	td := func() {
		err = teardownDB(db, testDB)
		if err != nil {
			t.Fatalf("teardown error: %v", err)
		}
	}

	defer td()

	testFetchBucketHelpers(t)
	testInitDB(t)
	testDatabase(t, db)
	testAcceptedWork(t, db)
	testAccount(t, db)
	testJob(t, db)
	testShares(t, db)
	testLimiter(t)
	testPayment(t, db)
	testDifficulty(t)
	testEndpoint(t, db)
	testClient(t, db)
	testPaymentMgr(t, db)
	testChainState(t, db)
	testHub(t, db)
}
