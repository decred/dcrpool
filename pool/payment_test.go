// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"fmt"
	"math/big"
	"testing"
	"time"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
)

// makePaymentBundle creates a new payment bundle.
func makePaymentBundle(account string, count uint32, paymentAmount dcrutil.Amount) *PaymentBundle {
	bundle := newPaymentBundle(account)
	for idx := uint32(0); idx < count; idx++ {
		payment := NewPayment(account, paymentAmount, 0, 0)
		bundle.Payments = append(bundle.Payments, payment)
	}
	return bundle
}

func testPayPerShare(t *testing.T, db *bolt.DB) {
	now := time.Now()
	sixtyBefore := now.Add(-(time.Second * 60)).UnixNano()
	thirtyBefore := now.Add(-(time.Second * 30)).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)
	shareCount := 10
	expectedShareCount := 20
	expectedTotal := 80
	height := uint32(20)

	// Create shares for account x and y.
	for i := 0; i < shareCount; i++ {
		err := persistShare(db, xID, weight, sixtyBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}

		err = persistShare(db, yID, weight, thirtyBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	nowBytes := nanoToBigEndianBytes(now.UnixNano())
	sixtyBeforeBytes := nanoToBigEndianBytes(sixtyBefore)

	// Ensures the shares created for both account x and y are
	// eligible for selection.
	shares, err := PPSEligibleShares(db, sixtyBeforeBytes, nowBytes)
	if err != nil {
		t.Error(err)
	}

	if len(shares) != expectedShareCount {
		t.Fatalf("expected %v shares eligible, got %v.",
			expectedShareCount, len(shares))
	}

	amt, err := dcrutil.NewAmount(float64(expectedTotal))
	if err != nil {
		t.Fatal(err)
	}

	feePercent := 0.1
	err = PayPerShare(db, amt, feePercent, height,
		chaincfg.SimNetParams().CoinbaseMaturity)
	if err != nil {
		t.Error(err)
	}

	// Ensure the last payment created time was updated.
	var lastPmtCreatedOn []byte
	err = db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
			return MakeError(ErrBucketNotFound, desc, nil)
		}

		v := pbkt.Get(lastPaymentCreatedOn)
		lastPmtCreatedOn = make([]byte, len(v))
		copy(lastPmtCreatedOn, v)

		return nil
	})
	if err != nil {
		t.Error(err)
	}

	if bytes.Compare(lastPmtCreatedOn, nowBytes) < 0 {
		t.Fatalf("the last payment created on time" +
			" is less than the current time")
	}

	// Ensure the payments created are for accounts x, y and a fee
	// payment entry.
	pmts, err := FetchPendingPayments(db)
	if err != nil {
		t.Error(err)
	}

	expectedBundleCount := 3
	bundles := generatePaymentBundles(pmts)
	if len(bundles) != expectedBundleCount {
		t.Fatalf("expected %v payment bundles, got %v.",
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
		if bundles[idx].Account == poolFeesK {
			fb = bundles[idx]
		}
	}

	// Ensure the two account payment bundles have the same payments since
	// they have the same share weights.
	if xb.Total() != yb.Total() {
		t.Fatalf("expected equal account amounts, %v != %v",
			xb.Total(), yb.Total())
	}

	// Ensure the fee payment is the exact fee percentage of the total amount.
	expectedFeeAmt := amt.MulF64(feePercent)
	if fb.Total() != expectedFeeAmt {
		t.Fatalf("expected %v fee payment amount, got %v",
			fb.Total(), expectedFeeAmt)
	}

	// Ensure the sum of all payment bundle amounts is equal to the initial
	// amount.
	sum := xb.Total() + yb.Total() + fb.Total()
	if sum != amt {
		t.Fatalf("expected the sum of all payments to be %v, got %v", amt, sum)
	}

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Empty the payment bucket.
	err = emptyBucket(db, paymentBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}
}

func testPayPerLastShare(t *testing.T, db *bolt.DB) {
	now := time.Now()
	sixtyBefore := now.Add(-(time.Second * 60)).UnixNano()
	thirtyBefore := now.Add(-(time.Second * 30)).UnixNano()
	tenBefore := now.Add(-(time.Second * 10)).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)
	shareCount := 5
	expectedShareCount := 10
	expectedTotal := 80
	height := uint32(0)

	// Create shares for account x and y.
	for i := 0; i < shareCount; i++ {
		err := persistShare(db, xID, weight, thirtyBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}

		err = persistShare(db, yID, weight, tenBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	nowBytes := nanoToBigEndianBytes(now.UnixNano())
	sixtyBeforeBytes := nanoToBigEndianBytes(sixtyBefore)

	// Ensure the shares created are eligible for selection.
	shares, err := PPLNSEligibleShares(db, sixtyBeforeBytes)
	if err != nil {
		t.Error(err)
	}

	if len(shares) != expectedShareCount {
		t.Fatalf("expected %v PPLNS shares eligible, got %v.",
			expectedShareCount, len(shares))
	}

	amt, err := dcrutil.NewAmount(float64(expectedTotal))
	if err != nil {
		t.Fatal(err)
	}

	feePercent := 0.1
	periodSecs := uint32(50) // 50 seconds.
	err = PayPerLastNShares(db, amt, feePercent, height,
		chaincfg.SimNetParams().CoinbaseMaturity, periodSecs)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure the last payment created time was updated.
	var lastPmtCreatedOn []byte
	err = db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
			return MakeError(ErrBucketNotFound, desc, nil)
		}

		v := pbkt.Get(lastPaymentCreatedOn)
		lastPmtCreatedOn = make([]byte, len(v))
		copy(lastPmtCreatedOn, v)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Compare(lastPmtCreatedOn, nowBytes) < 0 {
		t.Fatalf("the last payment created on time is less than " +
			"the current time")
	}

	// Ensure the payments created are for accounts x, y and a fee
	// payment entry.
	pmts, err := FetchPendingPayments(db)
	if err != nil {
		t.Fatal(err)
	}

	expectedBundleCount := 3
	bundles := generatePaymentBundles(pmts)

	if len(bundles) != expectedBundleCount {
		t.Fatalf("expected %v payment bundles, got %v.",
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
		if bundles[idx].Account == poolFeesK {
			fb = bundles[idx]
		}
	}

	// Ensure the two account payment bundles have the same payments since
	// they have the same share weights.
	if xb.Total() != yb.Total() {
		t.Fatalf("expected equal account amounts, %v != %v",
			xb.Total(), yb.Total())
	}

	// Ensure the fee payment is the exact fee percentage of the total amount.
	expectedFeeAmt := amt.MulF64(feePercent)
	if fb.Total() != expectedFeeAmt {
		t.Fatalf("expected %v fee payment amount, got %v",
			fb.Total(), expectedFeeAmt)
	}

	// Ensure the sum of all payment bundle amounts is equal to the initial
	// amount.
	sum := xb.Total() + yb.Total() + fb.Total()
	if sum != amt {
		t.Fatalf("expected the sum of all payments to be %v, got %v", amt, sum)
	}

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Empty the payment bucket.
	err = emptyBucket(db, paymentBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}
}

func testGeneratePaymentDetails(t *testing.T, db *bolt.DB) {
	count := uint32(3)
	pmtAmt, _ := dcrutil.NewAmount(10.5)
	bundleX := makePaymentBundle(xID, count, pmtAmt)
	bundleY := makePaymentBundle(yID, count, pmtAmt)
	expectedTotal := pmtAmt.MulF64(6)

	bundles := make([]*PaymentBundle, 0)
	bundles = append(bundles, bundleX)
	bundles = append(bundles, bundleY)
	zeroAmt := dcrutil.Amount(0)
	txFeeReserve := dcrutil.Amount(0)

	details, totalAmt, err := generatePaymentDetails(db,
		[]dcrutil.Address{poolFeeAddrs}, bundles, zeroAmt, &txFeeReserve)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure the payment total is as expected.
	if expectedTotal != *totalAmt {
		t.Fatalf("expected %v as total payment amount, got %v",
			expectedTotal, *totalAmt)
	}

	// Ensure the payment details generated are for only account x and y.
	if len(details) != 2 {
		t.Fatalf("expected %v payment details generated, got %v",
			2, len(details))
	}

	// Ensure the payment total for account x is equal to account y.
	xAmt := details[xAddr]
	yAmt := details[yAddr]
	if xAmt != yAmt {
		t.Fatalf("expected equal payment amounts for both accounts,"+
			" got %v != %v", xAmt, yAmt)
	}

	count = uint32(2)
	accXAmt, _ := dcrutil.NewAmount(9.5)
	accYAmt, _ := dcrutil.NewAmount(4)
	bundleX = makePaymentBundle(xID, count, accXAmt)
	bundleY = makePaymentBundle(yID, count, accYAmt)
	xTotal := accXAmt.MulF64(2)
	yTotal := accYAmt.MulF64(2)
	expectedTotal = xTotal + yTotal

	bundles = make([]*PaymentBundle, 0)
	bundles = append(bundles, bundleX)
	bundles = append(bundles, bundleY)

	details, totalAmt, err = generatePaymentDetails(db,
		[]dcrutil.Address{poolFeeAddrs}, bundles, zeroAmt, &txFeeReserve)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure the payment total is as expected.
	if expectedTotal != *totalAmt {
		t.Fatalf("expected %v as total payment amount, got %v",
			expectedTotal, *totalAmt)
	}

	// Ensure the payment details generated are for only account x and y.
	if len(details) != 2 {
		t.Fatalf("expected %v payment details generated, got %v",
			2, len(details))
	}

	xAmt = details[xAddr]
	yAmt = details[yAddr]

	// Ensure the payment amount for account x is as expected.
	if xAmt != xTotal {
		t.Fatalf("Expected %v for account X, got %v", yTotal, xAmt)
	}

	// Ensure the payment amount for account y is as expected.
	if yAmt != yTotal {
		t.Fatalf("Expected %v for account Y, got %v", yTotal, yAmt)
	}
}

func testPaymentsMaturity(t *testing.T, db *bolt.DB) {
	weight := new(big.Rat).SetFloat64(1.0)
	shareCount := 5
	height := uint32(20)
	feePercent := 0.1
	amt, err := dcrutil.NewAmount(80)
	if err != nil {
		t.Error(err)
	}

	// Create readily available payments for account X.
	now := time.Now().UnixNano()
	for i := 0; i < shareCount; i++ {
		err = persistShare(db, xID, weight, now)
		if err != nil {
			t.Fatal(err)
		}
	}

	err = PayPerShare(db, amt, feePercent, height, 0)
	if err != nil {
		t.Error(err)
	}

	time.Sleep(time.Millisecond * 50)

	// Create immature payments for account Y.
	now = time.Now().UnixNano()
	for i := 0; i < shareCount; i++ {
		err = persistShare(db, yID, weight, now)
		if err != nil {
			t.Fatal(err)
		}
	}

	err = PayPerShare(db, amt, feePercent, height,
		chaincfg.SimNetParams().CoinbaseMaturity)
	if err != nil {
		t.Fatal(err)
	}

	paymentReqs := make(map[string]struct{})
	pmts, err := FetchEligiblePaymentBundles(db, height, dcrutil.Amount(0), paymentReqs)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure the expected bundle count is as expected.
	expectedBundleCount := 2
	if len(pmts) != expectedBundleCount {
		t.Fatalf("expected %v payment bundles, got %v",
			expectedBundleCount, len(pmts))
	}

	var accXBundle, accYBundle, feeBundle *PaymentBundle
	for idx := 0; idx < len(pmts); idx++ {
		if pmts[idx].Account == xID {
			accXBundle = pmts[idx]
		}
		if pmts[idx].Account == yID {
			accYBundle = pmts[idx]
		}
		if pmts[idx].Account == poolFeesK {
			feeBundle = pmts[idx]
		}
	}

	// Ensure there are no bundles for account Y.
	if accYBundle != nil {
		t.Fatalf("expected no payment bundles for account Y")
	}

	// Ensure the bundle amounts are as expected.
	expectedXAmt := amt - amt.MulF64(feePercent)
	if accXBundle.Total() != expectedXAmt {
		t.Errorf("expected account X's bundle to have %v, got %v",
			expectedXAmt, accXBundle.Total())
	}

	expectedFeeAmt := amt.MulF64(feePercent)
	if feeBundle.Total() != expectedFeeAmt {
		t.Errorf("expected pool fee bundle to have %v, got %v",
			expectedFeeAmt, feeBundle.Total())
	}

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Empty the payment bucket.
	err = emptyBucket(db, paymentBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}
}

func testArchivedPaymentsFiltering(t *testing.T, db *bolt.DB) {
	count := uint32(2)
	amt, _ := dcrutil.NewAmount(5)

	bx := makePaymentBundle(xID, count, amt)
	bx.UpdateAsPaid(db, 10, "")
	err := bx.ArchivePayments(db)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Millisecond * 10)

	bx = makePaymentBundle(yID, count, amt)
	bx.UpdateAsPaid(db, 10, "")
	err = bx.ArchivePayments(db)
	if err != nil {
		t.Fatal(err)
	}

	// Fetch archived payments for account X.
	pmts, err := fetchArchivedPaymentsForAccount(db, xID, 10)
	if err != nil {
		t.Fatal(err)
	}

	expectedPmts := 0
	if len(pmts) != expectedPmts {
		t.Logf("expected %v archived payments for account X "+
			"(per filter criteria), got %v", expectedPmts, len(pmts))
	}

	// Fetch archived payments for account Y.
	pmts, err = fetchArchivedPaymentsForAccount(db, yID, 10)
	if err != nil {
		t.Error(err)
	}

	expectedPmts = 2
	if len(pmts) != expectedPmts {
		t.Logf("expected %v archived payments for account x"+
			" (per filter criteria), got %v", expectedPmts, len(pmts))
	}

	// Empty the payment archive bucket.
	err = emptyBucket(db, paymentArchiveBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}
}

func testMinimumPayment(t *testing.T, db *bolt.DB) {
	weight := new(big.Rat).SetFloat64(1.0)
	xShareCount := 10
	yShareCount := 5
	height := uint32(20)
	feePercent := 0.1
	amt, err := dcrutil.NewAmount(100)
	if err != nil {
		t.Error(err)
	}

	now := time.Now().UnixNano()
	// Create shares for account x and Y.
	for i := 0; i < xShareCount; i++ {
		err := persistShare(db, xID, weight, now+int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	now = time.Now().UnixNano()
	for i := 0; i < yShareCount; i++ {
		err := persistShare(db, yID, weight, now+int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	// Create readily available payments.
	err = PayPerShare(db, amt, feePercent, height, 0)
	if err != nil {
		t.Error(err)
	}

	// Make minimum payment greater than account Y's payment.
	ratio := (float64(yShareCount) / float64(xShareCount+yShareCount))
	minPayment := amt.MulF64(ratio)
	if err != nil {
		t.Error(err)
	}

	paymentReqs := make(map[string]struct{})
	pmts, err := FetchEligiblePaymentBundles(db, height, minPayment, paymentReqs)
	if err != nil {
		t.Error(err)
	}

	expectedBundleCount := 1
	if len(pmts) != expectedBundleCount {
		t.Errorf("Expected %v payment bundles, got %v", expectedBundleCount, len(pmts))
	}

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Empty the payment bucket.
	err = emptyBucket(db, paymentBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}
}

func testPaymentRequest(t *testing.T, db *bolt.DB) {
	weight := new(big.Rat).SetFloat64(1.0)
	xShareCount := 10
	yShareCount := 5
	height := uint32(20)
	feePercent := 0.1
	amt, err := dcrutil.NewAmount(100)
	if err != nil {
		t.Error(err)
	}

	now := time.Now().UnixNano()
	// Create shares for account x and Y.
	for i := 0; i < xShareCount; i++ {
		err := persistShare(db, xID, weight, now+int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	now = time.Now().UnixNano()
	for i := 0; i < yShareCount; i++ {
		err := persistShare(db, yID, weight, now+int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	// Create readily available payments.
	err = PayPerShare(db, amt, feePercent, height, 0)
	if err != nil {
		t.Error(err)
	}

	// Make minimum payment greater than account Y's payment.
	ratio := (float64(yShareCount) / float64(xShareCount+yShareCount))
	minPayment := amt.MulF64(ratio)
	if err != nil {
		t.Error(err)
	}

	// Create a payment request for account Y.
	paymentReqs := make(map[string]struct{})
	paymentReqs[yID] = struct{}{}

	pmts, err := FetchEligiblePaymentBundles(db, height, minPayment, paymentReqs)
	if err != nil {
		t.Error(err)
	}

	// Ensure the requested payment for account Y is returned as eligible.
	expectedBundleCount := 2
	if len(pmts) != expectedBundleCount {
		t.Errorf("Expected %v payment bundles, got %v", expectedBundleCount, len(pmts))
	}

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Empty the payment bucket.
	err = emptyBucket(db, paymentBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}
}
