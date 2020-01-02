// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"testing"
	"time"

	bolt "github.com/coreos/bbolt"
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
	expectedPmts := 2
	if len(pmts) != expectedPmts {
		t.Fatalf("expected %v archived payments for account X "+
			"(per filter criteria), got %v", expectedPmts, len(pmts))
	}

	// Fetch archived payments for account Y.
	pmts, err = fetchArchivedPaymentsForAccount(db, yID, 10)
	if err != nil {
		t.Error(err)
	}
	expectedPmts = 2
	if len(pmts) != expectedPmts {
		t.Fatalf("expected %v archived payments for account x"+
			" (per filter criteria), got %v", expectedPmts, len(pmts))
	}

	// Empty the payment archive bucket.
	err = emptyBucket(db, paymentArchiveBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}
}
