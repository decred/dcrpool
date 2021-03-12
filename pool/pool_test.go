package pool

import (
	"os"
	"testing"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
)

var (
	// testDB represents the database used in testing.
	testDB = "testdb"
	// Account X address.
	xAddr = "SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc"
	// Account X id.
	xID = NewAccount(xAddr).UUID
	// Account Y address.
	yAddr = "Ssp7J7TUmi5iPhoQnWYNGQbeGhu6V3otJcS"
	// Account Y id.
	yID = NewAccount(yAddr).UUID
	// Pool fee address.
	poolFeeAddrs, _ = dcrutil.DecodeAddress(
		"SsnbEmxCVXskgTHXvf3rEa17NA39qQuGHwQ",
		chaincfg.SimNetParams())

	db Database
)

func setupPostgresDB() (*PostgresDB, error) {
	pgHost := "127.0.0.1"
	pgPort := uint32(5432)
	pgUser := "dcrpooluser"
	pgPass := "12345"
	pgDBName := "dcrpooltestdb"
	purgeDB := false
	return InitPostgresDB(pgHost, pgPort, pgUser, pgPass, pgDBName, purgeDB)
}

// setupBoltDB initializes a bolt database.
func setupBoltDB() (*BoltDB, error) {
	os.Remove(testDB)
	var err error
	db, err := openBoltDB(testDB)
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

	return db, err
}

// teardownBoltDB closes the connection to the db and deletes the db file.
func teardownBoltDB(db *BoltDB, dbPath string) error {
	err := db.Close()
	if err != nil {
		return err
	}
	return os.Remove(dbPath)
}

// TestPool runs all pool related tests which require a real database.
func TestPool(t *testing.T) {

	// All sub-tests to run. All of these tests will be run with a postgres
	// database and a bolt database.
	tests := map[string]func(*testing.T){
		"testCSRFSecret":             testCSRFSecret,
		"testLastPaymentInfo":        testLastPaymentInfo,
		"testLastPaymentCreatedOn":   testLastPaymentCreatedOn,
		"testPoolMode":               testPoolMode,
		"testAcceptedWork":           testAcceptedWork,
		"testAccount":                testAccount,
		"testJob":                    testJob,
		"testDeleteJobsBeforeHeight": testDeleteJobsBeforeHeight,
		"testShares":                 testShares,
		"testPPSEligibleShares":      testPPSEligibleShares,
		"testPPLNSEligibleShares":    testPPLNSEligibleShares,
		"testPruneShares":            testPruneShares,
		"testPayment":                testPayment,
		"testArchivePayment":         testArchivePayment,
		"testPaymentAccessors":       testPaymentAccessors,
		"testEndpoint":               testEndpoint,
		"testClientHashCalc":         testClientHashCalc,
		"testClientRolledWork":       testClientTimeRolledWork,
		"testClientMessageHandling":  testClientMessageHandling,
		"testClientUpgrades":         testClientUpgrades,
		"testHashData":               testHashData,
		"testPaymentMgrPPS":          testPaymentMgrPPS,
		"testPaymentMgrPPLNS":        testPaymentMgrPPLNS,
		"testPaymentMgrMaturity":     testPaymentMgrMaturity,
		"testPaymentMgrPayment":      testPaymentMgrPayment,
		"testPaymentMgrDust":         testPaymentMgrDust,
		"testChainState":             testChainState,
		"testHub":                    testHub,
	}

	// Run all tests with bolt DB.
	for testName, test := range tests {
		boltDB, err := setupBoltDB()
		if err != nil {
			t.Fatalf("setupBoltDB error: %v", err)
		}

		db = boltDB

		t.Run(testName+"_Bolt", test)

		err = boltDB.purge()
		if err != nil {
			t.Fatalf("bolt teardown error: %v", err)
		}

		boltDB.Close()
	}

	// Run all tests with postgres DB.
	for testName, test := range tests {
		postgresDB, err := setupPostgresDB()
		if err != nil {
			t.Fatalf("setupPostgresDB error: %v", err)
		}

		db = postgresDB

		t.Run(testName+"_Postgres", test)

		err = postgresDB.purge()
		if err != nil {
			t.Fatalf("postgres teardown error: %v", err)
		}

		postgresDB.Close()
	}
}
