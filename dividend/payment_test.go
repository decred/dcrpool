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
	shareCount := 10
	height := uint32(20)

	err = createMultiplePersistedShares(db, accX, weight, minNano, shareCount)
	if err != nil {
		t.Error(t)
	}

	err = createMultiplePersistedShares(db, accY, weight, maxNano, shareCount)
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
	err = db.View(func(tx *bolt.Tx) error {
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
		if bundles[idx].Account == accX {
			xb = bundles[idx]
		}

		if bundles[idx].Account == accY {
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
	shareCount := 10
	height := uint32(20)

	err = createMultiplePersistedShares(db, accX, weight, minNano, shareCount)
	if err != nil {
		t.Error(t)
	}

	err = createMultiplePersistedShares(db, accY, weight, maxNano, shareCount)
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
	err = db.View(func(tx *bolt.Tx) error {
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
		if bundles[idx].Account == accX {
			xb = bundles[idx]
		}

		if bundles[idx].Account == accY {
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

// CreatePaymentBundle instantiates a payment bundle.
func CreatePaymentBundle(account string, count uint32, paymentAmount dcrutil.Amount) *PaymentBundle {
	bundle := NewPaymentBundle(account)
	for idx := uint32(0); idx < count; idx++ {
		payment := NewPayment(account, paymentAmount, 0, 0)
		bundle.Payments = append(bundle.Payments, payment)
	}
	return bundle
}

func TestEqualPaymentDetailsGeneration(t *testing.T) {
	db, err := setupDB()
	if err != nil {
		t.Error(t)
	}

	count := uint32(3)
	pmtAmt, _ := dcrutil.NewAmount(10.5)
	bundleX := CreatePaymentBundle(accX, count, pmtAmt)
	bundleY := CreatePaymentBundle(accY, count, pmtAmt)
	expectedTotal := pmtAmt.MulF64(6)

	bundles := make([]*PaymentBundle, 0)
	bundles = append(bundles, bundleX)
	bundles = append(bundles, bundleY)

	details, totalAmt, err := GeneratePaymentDetails(db,
		[]dcrutil.Address{poolFeeAddrs}, bundles)
	if err != nil {
		t.Error(err)
	}

	if expectedTotal != *totalAmt {
		t.Errorf("Expected %v as total payment amount, got %v",
			expectedTotal, *totalAmt)
	}

	if len(details) != 2 {
		t.Errorf("Expected %v payment details generated, got %v",
			2, len(details))
	}

	xAmt := details[xAddr]
	yAmt := details[yAddr]
	if xAmt != yAmt {
		t.Errorf("Expected equal payment amounts for both accounts,"+
			" got %v != %v", xAmt, yAmt)
	}

	err = teardownDB(db)
	if err != nil {
		t.Error(t)
	}
}

func TestUnequalPaymentDetailsGeneration(t *testing.T) {
	db, err := setupDB()
	if err != nil {
		t.Error(t)
	}

	count := uint32(2)
	accXAmt, _ := dcrutil.NewAmount(9.5)
	accYAmt, _ := dcrutil.NewAmount(4)
	bundleX := CreatePaymentBundle(accX, count, accXAmt)
	bundleY := CreatePaymentBundle(accY, count, accYAmt)
	xTotal := accXAmt.MulF64(2)
	yTotal := accYAmt.MulF64(2)
	expectedTotal := xTotal + yTotal

	bundles := make([]*PaymentBundle, 0)
	bundles = append(bundles, bundleX)
	bundles = append(bundles, bundleY)

	details, totalAmt, err := GeneratePaymentDetails(db,
		[]dcrutil.Address{poolFeeAddrs}, bundles)
	if err != nil {
		t.Error(err)
	}

	if expectedTotal != *totalAmt {
		t.Errorf("Expected %v as total payment amount, got %v",
			expectedTotal, *totalAmt)
	}

	if len(details) != 2 {
		t.Errorf("Expected %v payment details generated, got %v",
			2, len(details))
	}

	xAmt := details[xAddr]
	yAmt := details[yAddr]

	if xAmt != xTotal {
		t.Errorf("Expected %v for account X, got %v", yTotal, xAmt)
	}

	if yAmt != yTotal {
		t.Errorf("Expected %v for account Y, got %v", yTotal, yAmt)
	}

	err = teardownDB(db)
	if err != nil {
		t.Error(t)
	}
}

// Then create another test that creates eligible payments and ineligible
// ones to check the eligibity calculations.

func TestImmaturePayments(t *testing.T) {
	db, err := setupDB()
	if err != nil {
		t.Error(t)
	}

	weight := new(big.Rat).SetFloat64(1.0)
	shareCount := 10
	height := uint32(20)
	feePercent := 0.1
	amt, err := dcrutil.NewAmount(100.25)
	if err != nil {
		t.Error(t)
	}

	xNano := time.Now().UnixNano()
	err = createMultiplePersistedShares(db, accX, weight, xNano, shareCount)
	if err != nil {
		t.Error(t)
	}

	// Create readily available payments for acount X.
	err = PayPerShare(db, amt, feePercent, height, 0)
	if err != nil {
		t.Error(t)
	}

	time.Sleep(time.Second * 2)

	yNano := time.Now().UnixNano()
	err = createMultiplePersistedShares(db, accY, weight, yNano, shareCount)
	if err != nil {
		t.Error(t)
	}

	// Create immature payments for acount Y.
	err = PayPerShare(db, amt, feePercent, height,
		chaincfg.SimNetParams.CoinbaseMaturity)
	if err != nil {
		t.Error(t)
	}

	minPayment, err := dcrutil.NewAmount(0.2)
	if err != nil {
		t.Error(t)
	}

	pmts, err := FetchEligiblePayments(db, minPayment)
	if err != nil {
		t.Error(t)
	}

	if len(pmts) > 2 {
		t.Errorf("Expected %v payment bundles, got %v", 2, len(pmts))
	}

	var accXBundle, feeBundle *PaymentBundle
	for idx := 0; idx < len(pmts); idx++ {
		if pmts[idx].Account == accX {
			accXBundle = pmts[idx]
		}

		if pmts[idx].Account == PoolFeesK {
			feeBundle = pmts[idx]
		}
	}

	expectedXAmt := amt - amt.MulF64(feePercent)
	if accXBundle.Total() != expectedXAmt {
		t.Errorf("Expected account X's bundle to have %v, got %v",
			expectedXAmt, accXBundle.Total())
	}

	expectedFeeAmt := amt.MulF64(feePercent)
	if feeBundle.Total() != expectedFeeAmt {
		t.Errorf("Expected pool fee's bundle to have %v, got %v",
			expectedFeeAmt, feeBundle.Total())
	}

	err = teardownDB(db)
	if err != nil {
		t.Error(t)
	}
}
