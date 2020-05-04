// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"fmt"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	bolt "go.etcd.io/bbolt"
)

func persistPayment(db *bolt.DB, account string, source *PaymentSource,
	amount dcrutil.Amount, height uint32, estMaturity uint32) (*Payment, error) {
	pmt := NewPayment(account, source, amount, height, estMaturity)
	err := pmt.Create(db)
	if err != nil {
		return nil, fmt.Errorf("unable to persist payment: %v", err)
	}
	return pmt, nil
}

func testPayment(t *testing.T, db *bolt.DB) {
	height := uint32(10)
	estMaturity := uint32(26)
	zeroSource := &PaymentSource{
		BlockHash: chainhash.Hash{0}.String(),
		Coinbase:  chainhash.Hash{0}.String(),
	}

	amt, _ := dcrutil.NewAmount(5)
	pmtA, err := persistPayment(db, xID, zeroSource, amt, height, estMaturity)
	if err != nil {
		t.Fatal(err)
	}

	pmtB, err := persistPayment(db, xID, zeroSource, amt, height, estMaturity)
	if err != nil {
		t.Fatal(err)
	}

	pmtC, err := persistPayment(db, yID, zeroSource, amt, height, estMaturity)
	if err != nil {
		t.Fatal(err)
	}

	// Fetch a payment using its id.
	id := paymentID(pmtA.Height, pmtA.CreatedOn, pmtA.Account)
	fetchedPayment, err := FetchPayment(db, id)
	if err != nil {
		t.Fatalf("FetchPayment err: %v", err)
	}
	if fetchedPayment == nil {
		t.Fatal("expected a non-nil payment")
	}

	// Ensure payments can be updated.
	txid := chainhash.Hash{0}.String()
	pmtB.TransactionID = txid
	err = pmtB.Update(db)
	if err != nil {
		t.Fatalf("payment update err: %v", err)
	}

	id = paymentID(pmtB.Height, pmtB.CreatedOn, pmtB.Account)
	fetchedPayment, err = FetchPayment(db, id)
	if err != nil {
		t.Fatalf("FetchPayment err: %v", err)
	}

	if fetchedPayment.TransactionID != txid {
		t.Fatalf("expected payment with tx id %s, got %s",
			txid, fetchedPayment.TransactionID)
	}

	// Persist payment B as an archived payment.
	pmtB.PaidOnHeight = estMaturity + 1
	err = pmtB.Archive(db)
	if err != nil {
		t.Fatalf("payment delete error: %v", err)
	}

	// Ensure the payment B was archived.
	id = paymentID(pmtB.Height, pmtB.CreatedOn, pmtB.Account)
	_, err = FetchPayment(db, id)
	if err == nil {
		t.Fatalf("expected a value not found error: %v", err)
	}

	// Delete payment C.
	err = pmtC.Delete(db)
	if err != nil {
		t.Fatalf("payment delete error: %v", err)
	}

	// Ensure the payment C was deleted.
	id = paymentID(pmtC.Height, pmtC.CreatedOn, pmtC.Account)
	fetchedPayment, err = FetchPayment(db, id)
	if err == nil {
		t.Fatalf("expected a value not found error: %v", err)
	}

	if fetchedPayment != nil {
		t.Fatal("expected a nil payment")
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
