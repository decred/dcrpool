package pool

import (
	"fmt"
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
	xID = NewAccount(xAddr).UUID
	// Account Y address.
	yAddr = "Ssp7J7TUmi5iPhoQnWYNGQbeGhu6V3otJcS"
	// Account Y id.
	yID = NewAccount(yAddr).UUID
	// Pool fee address.
	poolFeeAddrs, _ = dcrutil.DecodeAddress(
		"SsnbEmxCVXskgTHXvf3rEa17NA39qQuGHwQ",
		chaincfg.SimNetParams())

	db *BoltDB
)

// setupDB initializes the pool database.
func setupDB() error {
	os.Remove(testDB)
	var err error
	db, err = openBoltDB(testDB)
	if err != nil {
		return err
	}

	err = createBuckets(db)
	if err != nil {
		return err
	}
	err = upgradeDB(db)
	if err != nil {
		return err
	}

	return err
}

// teardownBoltDB closes the connection to the db and deletes the db file.
func teardownBoltDB(db *BoltDB, dbPath string) error {
	err := db.Close()
	if err != nil {
		return err
	}
	return os.Remove(dbPath)
}

// emptyBucket deletes all k/v pairs in the provided bucket.
func emptyBucket(db *BoltDB, bucket []byte) error {
	const funcName = "emptyBucket"
	return db.DB.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("%s: bucket %s not found", funcName,
				string(poolBkt))
			return dbError(ErrBucketNotFound, desc)
		}
		b := pbkt.Bucket(bucket)
		toDelete := [][]byte{}
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			toDelete = append(toDelete, k)
		}
		for _, k := range toDelete {
			err := b.Delete(k)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// TestPool runs all pool related tests which require a real database.
// An clean instance of bbolt is created and initialized with buckets before
// each test.
func TestPool(t *testing.T) {

	// All sub-tests to run.
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
		"testPaymentAccessors":       testPaymentAccessors,
		"testEndpoint":               testEndpoint,
		"testClient":                 testClient,
		"testPaymentMgrPPS":          testPaymentMgrPPS,
		"testPaymentMgrPPLNS":        testPaymentMgrPPLNS,
		"testPaymentMgrMaturity":     testPaymentMgrMaturity,
		"testPaymentMgrPayment":      testPaymentMgrPayment,
		"testPaymentMgrDust":         testPaymentMgrDust,
		"testChainState":             testChainState,
		"testHub":                    testHub,
	}

	for testName, test := range tests {
		// Create a new blank database for each sub-test.
		var err error
		err = setupDB()
		if err != nil {
			t.Fatalf("setup error: %v", err)
		}

		// Run the sub-test.
		t.Run(testName, test)

		// Remove database.
		err = teardownBoltDB(db, testDB)
		if err != nil {
			t.Fatalf("teardown error: %v", err)
		}
	}

}
