package dividend

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"

	"dnldd/dcrpool/database"
)

// createShare creates a share with the provided account, weight and
// created on time.
func createShare(db *bolt.DB, account string, weight *big.Rat, createdOnNano int64) *Share {
	share := &Share{
		Account:   account,
		Weight:    weight,
		CreatedOn: createdOnNano,
	}

	return share
}

// createPersistedAccount creates a pool account with the provided parameters
// and persists it to the database.
func createPersistedAccount(db *bolt.DB, name string, address string, pass string) error {
	account, err := NewAccount(name, address, pass)
	if err != nil {
		return err
	}

	return account.Create(db)
}

// createMultipleShares creates multiple shares per the count provided.
func createMultipleShares(db *bolt.DB, account string, weight *big.Rat,
	createdOnNano int64, count int) []*Share {
	shares := make([]*Share, 0)
	for idx := 0; idx < count; idx++ {
		share := createShare(db, account, weight, createdOnNano+int64(idx))
		shares = append(shares, share)
	}

	return shares
}

func TestPayPerShare(t *testing.T) {
	db, err := setupDB()
	if err != nil {
		t.Error(t)
	}

	now := time.Now()
	nowBytes := NanoToBigEndianBytes(now.UnixNano())
	minNano := pastTime(&now, 0, 0, time.Duration(60), 0).UnixNano()
	minBytes := NanoToBigEndianBytes(minNano)
	maxNano := pastTime(&now, 0, 0, time.Duration(30), 0).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)
	x := "dnldd"
	y := "tmmy"
	aAddr := "SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc"
	bAddr := "Ssp7J7TUmi5iPhoQnWYNGQbeGhu6V3otJcS"
	pass := "pass"
	shareCount := 10
	height := uint32(20)

	err = createPersistedAccount(db, x, aAddr, pass)
	if err != nil {
		t.Error(t)
	}

	err = createPersistedAccount(db, y, bAddr, pass)
	if err != nil {
		t.Error(t)
	}

	err = createMultiplePersistedShares(db, x, weight, minNano, shareCount)
	if err != nil {
		t.Error(t)
	}

	err = createMultiplePersistedShares(db, y, weight, maxNano, shareCount)
	if err != nil {
		t.Error(t)
	}

	// Assert the shares created are eligible for selection.
	shares, err := FetchEligibleShares(db, minBytes, nowBytes)
	if err != nil {
		t.Error(t)
	}

	if len(shares) != 20 {
		t.Errorf("Expected %v shares eligible, got %v.", 20, len(shares))
	}

	amt, err := dcrutil.NewAmount(100.25)
	if err != nil {
		t.Error(t)
	}

	feePercent := 0.1
	err = PayPerShare(db, amt, feePercent, height,
		chaincfg.SimNetParams.CoinbaseMaturity)
	if err != nil {
		t.Error(t)
	}

	// Assert the last payment created time was updated.
	var lastPaymentCreatedOn []byte
	err = db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		lastPaymentCreatedOn = pbkt.Get(LastPaymentCreatedOn)
		return nil
	})

	if err != nil {
		t.Error(t)
	}

	if bytes.Compare(lastPaymentCreatedOn, nowBytes) < 0 {
		t.Error("The last payment created on time is less than" +
			" the current time")
	}

	// Assert the payments created are for accounts x, y and a fee
	// payment entry.
	pmts, err := FetchPendingPayments(db)
	if err != nil {
		t.Error(t)
	}

	bundles := GeneratePaymentBundles(pmts)
	if len(bundles) != 3 {
		t.Errorf("Expected %v payment bundles, got %v.", 3, len(bundles))
	}

	var xb, yb, fb *PaymentBundle
	for idx := 0; idx < len(bundles); idx++ {
		if bundles[idx].Account == x {
			xb = bundles[idx]
		}

		if bundles[idx].Account == y {
			yb = bundles[idx]
		}

		if bundles[idx].Account == PoolFeesK {
			fb = bundles[idx]
		}
	}

	// Assert the two account payment bundles have the same payments since
	// they have the same share weights.
	if xb.Total() != yb.Total() {
		t.Errorf("Expected equal account amounts, %v != %v",
			xb.Total(), yb.Total())
	}

	// Assert the fee payment is the exact fee percentage of the total amount.
	expectedFeeAmt := amt.MulF64(feePercent)
	if fb.Total() != expectedFeeAmt {
		t.Errorf("Expected %v fee payment amount, got %v",
			fb.Total(), expectedFeeAmt)
	}

	// Assert the sum of all payment bundle amounts is equal to the initial
	// amount.
	sum := xb.Total() + yb.Total() + fb.Total()
	if sum != amt {
		t.Errorf("Expected the sum of all payments to be %v, got %v", amt, sum)
	}

	err = teardownDB(db)
	if err != nil {
		t.Error(t)
	}
}

func TestPayPerLastShare(t *testing.T) {
	db, err := setupDB()
	if err != nil {
		t.Error(t)
	}

	now := time.Now()
	nowBytes := NanoToBigEndianBytes(now.UnixNano())
	minNano := pastTime(&now, 0, 0, time.Duration(60), 0).UnixNano()
	minBytes := NanoToBigEndianBytes(minNano)
	maxNano := pastTime(&now, 0, 0, time.Duration(30), 0).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)
	x := "dnldd"
	y := "tmmy"
	xAddr := "SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc"
	yAddr := "Ssp7J7TUmi5iPhoQnWYNGQbeGhu6V3otJcS"
	pass := "pass"
	shareCount := 10
	height := uint32(20)

	err = createPersistedAccount(db, x, xAddr, pass)
	if err != nil {
		t.Error(t)
	}

	err = createPersistedAccount(db, y, yAddr, pass)
	if err != nil {
		t.Error(t)
	}

	err = createMultiplePersistedShares(db, x, weight, minNano, shareCount)
	if err != nil {
		t.Error(t)
	}

	err = createMultiplePersistedShares(db, y, weight, maxNano, shareCount)
	if err != nil {
		t.Error(t)
	}

	// Assert the shares created are eligible for selection.
	shares, err := FetchEligibleShares(db, minBytes, nowBytes)
	if err != nil {
		t.Error(t)
	}

	if len(shares) != 20 {
		t.Errorf("Expected %v shares eligible, got %v.", 20, len(shares))
	}

	amt, err := dcrutil.NewAmount(100.25)
	if err != nil {
		t.Error(t)
	}

	feePercent := 0.1
	periodSecs := uint32(7200) // 2 hours.
	err = PayPerLastNShares(db, amt, feePercent, height,
		chaincfg.SimNetParams.CoinbaseMaturity, periodSecs)
	if err != nil {
		t.Error(t)
	}

	// Assert the last payment created time was updated.
	var lastPaymentCreatedOn []byte
	err = db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		lastPaymentCreatedOn = pbkt.Get(LastPaymentCreatedOn)
		return nil
	})

	if err != nil {
		t.Error(t)
	}

	if bytes.Compare(lastPaymentCreatedOn, nowBytes) < 0 {
		t.Error("The last payment created on time is less than" +
			"the current time")
	}

	// Assert the payments created are for accounts x, y and a fee
	// payment entry.
	pmts, err := FetchPendingPayments(db)
	if err != nil {
		t.Error(t)
	}

	bundles := GeneratePaymentBundles(pmts)
	if len(bundles) != 3 {
		t.Errorf("Expected %v payment bundles, got %v.", 3, len(bundles))
	}

	var xb, yb, fb *PaymentBundle
	for idx := 0; idx < len(bundles); idx++ {
		if bundles[idx].Account == x {
			xb = bundles[idx]
		}

		if bundles[idx].Account == y {
			yb = bundles[idx]
		}

		if bundles[idx].Account == PoolFeesK {
			fb = bundles[idx]
		}
	}

	// Assert the two account payment bundles have the same payments since
	// they have the same share weights.
	if xb.Total() != yb.Total() {
		t.Errorf("Expected equal account amounts, %v != %v",
			xb.Total(), yb.Total())
	}

	// Assert the fee payment is the exact fee percentage of the total amount.
	expectedFeeAmt := amt.MulF64(feePercent)
	if fb.Total() != expectedFeeAmt {
		t.Errorf("Expected %v fee payment amount, got %v",
			fb.Total(), expectedFeeAmt)
	}

	// Assert the sum of all payment bundle amounts is equal to the initial
	// amount.
	sum := xb.Total() + yb.Total() + fb.Total()
	if sum != amt {
		t.Errorf("Expected the sum of all payments to be %v, got %v", amt, sum)
	}

	err = teardownDB(db)
	if err != nil {
		t.Error(t)
	}
}
