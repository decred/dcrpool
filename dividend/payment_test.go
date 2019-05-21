// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dividend

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrpool/database"
	"github.com/decred/dcrpool/util"
)

// createPersistedAccount creates a pool account with the provided parameters
// and persists it to the database.
func createPersistedAccount(db *bolt.DB, address string) error {
	account, err := NewAccount(address)
	if err != nil {
		return err
	}

	return account.Create(db)
}

func TestPayPerShare(t *testing.T) {
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
	nowBytes := util.NanoToBigEndianBytes(now.UnixNano())
	minNano := now.Add(-(time.Second * 60)).UnixNano()
	minBytes := util.NanoToBigEndianBytes(minNano)
	maxNano := now.Add(-(time.Second * 30)).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)
	shareCount := 10
	expectedShareCount := 20
	expectedTotal := 100.25
	height := uint32(20)

	err = createMultiplePersistedShares(db, xID, weight, minNano, shareCount)
	if err != nil {
		t.Error(err)
	}

	err = createMultiplePersistedShares(db, yID, weight, maxNano, shareCount)
	if err != nil {
		t.Error(err)
	}

	// Assert the shares created are eligible for selection.
	shares, err := PPSEligibleShares(db, minBytes, nowBytes)
	if err != nil {
		t.Error(err)
	}

	if len(shares) != expectedShareCount {
		t.Errorf("Expected %v shares eligible, got %v.",
			expectedShareCount, len(shares))
	}

	amt, err := dcrutil.NewAmount(expectedTotal)
	if err != nil {
		t.Error(err)
	}

	feePercent := 0.1
	err = PayPerShare(db, amt, feePercent, height,
		chaincfg.SimNetParams.CoinbaseMaturity)
	if err != nil {
		t.Error(err)
	}

	// Assert the last payment created time was updated.
	var lastPaymentCreatedOn []byte
	err = db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		lastPaymentCreatedOn = pbkt.Get(database.LastPaymentCreatedOn)
		return nil
	})

	if err != nil {
		t.Error(err)
	}

	if bytes.Compare(lastPaymentCreatedOn, nowBytes) < 0 {
		t.Error("The last payment created on time is less than" +
			" the current time")
	}

	// Assert the payments created are for accounts x, y and a fee
	// payment entry.
	pmts, err := FetchPendingPayments(db)
	if err != nil {
		t.Error(err)
	}

	expectedBundleCount := 3
	bundles := GeneratePaymentBundles(pmts)
	if len(bundles) != expectedBundleCount {
		t.Errorf("Expected %v payment bundles, got %v.",
			expectedBundleCount, len(bundles))
	}

	var xb, yb, fb *PaymentBundle
	for idx := 0; idx < len(bundles); idx++ {
		if bundles[idx].Account == xID {
			xb = bundles[idx]
		}

		if bundles[idx].Account == yID {
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
}

func TestPayPerLastShare(t *testing.T) {
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
	nowBytes := util.NanoToBigEndianBytes(now.UnixNano())
	minNano := now.Add(-(time.Second * 60)).UnixNano()
	minBytes := util.NanoToBigEndianBytes(minNano)
	xAboveMinNano := now.Add(-(time.Second * 30)).UnixNano()
	yAboveMinNano := now.Add(-(time.Second * 10)).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)
	shareCount := 5
	expectedShareCount := 10
	expectedTotal := 100.25
	height := uint32(0)

	err = createMultiplePersistedShares(db, xID, weight, xAboveMinNano, shareCount)
	if err != nil {
		t.Error(err)
	}

	err = createMultiplePersistedShares(db, yID, weight, yAboveMinNano, shareCount)
	if err != nil {
		t.Error(err)
	}

	// Assert the shares created are eligible for selection.
	shares, err := PPLNSEligibleShares(db, minBytes)
	if err != nil {
		t.Error(err)
	}

	if len(shares) != expectedShareCount {
		t.Errorf("Expected %v PPLNS shares eligible, got %v.",
			expectedShareCount, len(shares))
	}

	amt, err := dcrutil.NewAmount(expectedTotal)
	if err != nil {
		t.Error(err)
	}

	feePercent := 0.1
	periodSecs := uint32(50) // 50 seconds.
	err = PayPerLastNShares(db, amt, feePercent, height,
		chaincfg.SimNetParams.CoinbaseMaturity, periodSecs)
	if err != nil {
		t.Error(err)
	}

	// Assert the last payment created time was updated.
	var lastPaymentCreatedOn []byte
	err = db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		lastPaymentCreatedOn = pbkt.Get(database.LastPaymentCreatedOn)
		return nil
	})

	if err != nil {
		t.Error(err)
	}

	if bytes.Compare(lastPaymentCreatedOn, nowBytes) < 0 {
		t.Error("The last payment created on time is less than" +
			"the current time")
	}

	// Assert the payments created are for accounts x, y and a fee
	// payment entry.
	pmts, err := FetchPendingPayments(db)
	if err != nil {
		t.Error(err)
	}

	expectedBundleCount := 3
	bundles := GeneratePaymentBundles(pmts)

	if len(bundles) != expectedBundleCount {
		t.Errorf("Expected %v payment bundles, got %v.",
			expectedBundleCount, len(bundles))
	}

	var xb, yb, fb *PaymentBundle
	for idx := 0; idx < len(bundles); idx++ {
		if bundles[idx].Account == xID {
			xb = bundles[idx]
		}

		if bundles[idx].Account == yID {
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
		t.Error(err)
	}

	td := func() {
		err = teardownDB(db)
		if err != nil {
			t.Error(err)
		}
	}

	defer td()

	count := uint32(3)
	pmtAmt, _ := dcrutil.NewAmount(10.5)
	bundleX := CreatePaymentBundle(xID, count, pmtAmt)
	bundleY := CreatePaymentBundle(yID, count, pmtAmt)
	expectedTotal := pmtAmt.MulF64(6)

	bundles := make([]*PaymentBundle, 0)
	bundles = append(bundles, bundleX)
	bundles = append(bundles, bundleY)
	zeroAmt := dcrutil.Amount(0)
	txFeeReserve := dcrutil.Amount(0)

	details, totalAmt, err := GeneratePaymentDetails(db,
		[]dcrutil.Address{poolFeeAddrs}, bundles, zeroAmt, &txFeeReserve)
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
}

func TestUnequalPaymentDetailsGeneration(t *testing.T) {
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

	count := uint32(2)
	accXAmt, _ := dcrutil.NewAmount(9.5)
	accYAmt, _ := dcrutil.NewAmount(4)
	bundleX := CreatePaymentBundle(xID, count, accXAmt)
	bundleY := CreatePaymentBundle(yID, count, accYAmt)
	xTotal := accXAmt.MulF64(2)
	yTotal := accYAmt.MulF64(2)
	expectedTotal := xTotal + yTotal

	bundles := make([]*PaymentBundle, 0)
	bundles = append(bundles, bundleX)
	bundles = append(bundles, bundleY)

	zeroAmt := dcrutil.Amount(0)
	txFeeReserve := dcrutil.Amount(0)

	details, totalAmt, err := GeneratePaymentDetails(db,
		[]dcrutil.Address{poolFeeAddrs}, bundles, zeroAmt, &txFeeReserve)
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
}

func TestPaymentsMaturity(t *testing.T) {
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

	weight := new(big.Rat).SetFloat64(1.0)
	shareCount := 10
	height := uint32(20)
	feePercent := 0.1
	amt, err := dcrutil.NewAmount(100.25)
	if err != nil {
		t.Error(err)
	}

	xNano := time.Now().UnixNano()
	err = createMultiplePersistedShares(db, xID, weight, xNano, shareCount)
	if err != nil {
		t.Error(err)
	}

	// Create readily available payments for acount X.
	err = PayPerShare(db, amt, feePercent, height, 0)
	if err != nil {
		t.Error(err)
	}

	time.Sleep(time.Millisecond * 200)

	yNano := time.Now().UnixNano()
	err = createMultiplePersistedShares(db, yID, weight, yNano, shareCount)
	if err != nil {
		t.Error(err)
	}

	// Create immature payments for acount Y.
	err = PayPerShare(db, amt, feePercent, height,
		chaincfg.SimNetParams.CoinbaseMaturity)
	if err != nil {
		t.Error(err)
	}

	minPayment, err := dcrutil.NewAmount(0.2)
	if err != nil {
		t.Error(err)
	}

	pmts, err := FetchEligiblePaymentBundles(db, height, minPayment)
	if err != nil {
		t.Error(err)
	}

	expectedBundleCount := 2
	if len(pmts) != expectedBundleCount {
		t.Errorf("Expected %v payment bundles, got %v", expectedBundleCount, len(pmts))
	}

	var accXBundle, feeBundle *PaymentBundle
	for idx := 0; idx < len(pmts); idx++ {
		if pmts[idx].Account == xID {
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
}

func TestArchivedPaymentsFiltering(t *testing.T) {
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

	count := uint32(2)
	amt, _ := dcrutil.NewAmount(5)

	bx := CreatePaymentBundle(xID, count, amt)
	bx.UpdateAsPaid(db, 10)
	bx.ArchivePayments(db)

	time.Sleep(time.Second * 10)

	bx = CreatePaymentBundle(yID, count, amt)
	bx.UpdateAsPaid(db, 10)
	bx.ArchivePayments(db)

	// Fetch archived payments for account x.
	pmts, err := FetchArchivedPaymentsForAccount(db, xID, 10)
	if err != nil {
		t.Error(err)
	}

	expectedPmts := 0
	if len(pmts) != expectedPmts {
		t.Logf("Expected %v archived payments for account x"+
			" (per filter criteria), got %v", expectedPmts, len(pmts))
	}

	// Fetch archived payments for account y.
	pmts, err = FetchArchivedPaymentsForAccount(db, yID, 10)
	if err != nil {
		t.Error(err)
	}

	expectedPmts = 2
	if len(pmts) != expectedPmts {
		t.Logf("Expected %v archived payments for account x"+
			" (per filter criteria), got %v", expectedPmts, len(pmts))
	}
}
