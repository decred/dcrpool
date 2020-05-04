// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	bolt "go.etcd.io/bbolt"
)

// makePaymentBundle creates a new payment bundle.
func makePaymentBundle(account string, count uint32, paymentAmount dcrutil.Amount) *PaymentBundle {
	source := &PaymentSource{
		BlockHash: chainhash.Hash{0}.String(),
		Coinbase:  chainhash.Hash{0}.String(),
	}

	bundle := newPaymentBundle(account)
	for idx := uint32(0); idx < count; idx++ {
		payment := NewPayment(account, source, paymentAmount, 0, 0)
		bundle.Payments = append(bundle.Payments, payment)
	}
	return bundle
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
	details, totalAmt, err := generatePaymentDetails(db, poolFeeAddrs, bundles)
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

	details, totalAmt, err = generatePaymentDetails(db, poolFeeAddrs, bundles)
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

func testAccountPayments(t *testing.T, db *bolt.DB) {
	count := uint32(2)
	amt, _ := dcrutil.NewAmount(5)

	// Create pending payments for account X.
	pbx := makePaymentBundle(xID, count, amt)
	for _, pmt := range pbx.Payments {
		err := pmt.Create(db)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Create archived payments for account X.
	abx := makePaymentBundle(xID, count, amt)
	abx.UpdateAsPaid(db, 10, "")
	err := abx.ArchivePayments(db)
	if err != nil {
		t.Fatal(err)
	}

	// Check fetching all archived payments.
	archived, err := fetchArchivedPayments(db)
	if err != nil {
		t.Fatal(err)
	}

	if len(archived) != 2 {
		t.Fatalf("expected archived payments to be %v, got %v", 2, archived)
	}

	// Check fetching pending payments at height.
	pmts, err := fetchPendingPaymentsAtHeight(db, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(pmts) == 2 {
		t.Fatalf("expected 2 pending payments at "+
			"height #%d, got %d", 10, len(pmts))
	}

	// Check fetching all pending payments.
	pending, err := fetchPendingPayments(db)
	if err != nil {
		t.Fatal(err)
	}

	if len(pending) != 2 {
		t.Fatalf("expected pending payments to be %v, got %v", 2, pending)
	}

	// Make a payment bundle from the pending payments.
	// Two payments for the same account should result in one bundle containing
	// two payments.
	bdls := generatePaymentBundles(pending)
	if len(bdls) != 1 {
		t.Fatalf("expected 1 payment bundle, got %d", len(bdls))
	}

	epb := bdls[0]
	if len(pending) != len(epb.Payments) {
		t.Fatalf("expected %d payments in bundle, got %d",
			len(pending), len(epb.Payments))
	}

	// Ensure fetching a non-existent payment returns an error.
	pmtID := paymentID(300, time.Now().UnixNano(), xID)
	_, err = GetPayment(db, pmtID)
	if err == nil {
		t.Fatal("[GetPayment] expected a value not found error")
	}

	// Fetching an existing payment.
	expectedPmt := pending[0]
	pmtID = paymentID(expectedPmt.Height, expectedPmt.CreatedOn,
		expectedPmt.Account)
	pmt, err := GetPayment(db, pmtID)
	if err != nil {
		t.Fatalf("[GetPayment] unexpected error: %v", err)
	}

	// Ensure the returned payment matches the expected.
	if expectedPmt.Height != pmt.Height {
		t.Fatalf("expected payment height %d, got %d",
			expectedPmt.Height, pmt.Height)
	}

	if expectedPmt.Account != pmt.Account {
		t.Fatalf("expected payment account %s, got %s",
			expectedPmt.Account, pmt.Account)
	}

	if expectedPmt.CreatedOn != pmt.CreatedOn {
		t.Fatalf("expected payment created on time %d, "+
			"got %d", expectedPmt.CreatedOn, pmt.CreatedOn)
	}

	// Persist an updated payment.
	pmt.PaidOnHeight = 100
	err = pmt.Update(db)
	if err != nil {
		t.Fatalf("[Update]: unexpected error: %v", err)
	}

	// Empty the payment bucket.
	err = emptyBucket(db, paymentBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Empty the payment archive bucket.
	err = emptyBucket(db, paymentArchiveBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}
}
