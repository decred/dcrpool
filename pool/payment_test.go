// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"fmt"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	bolt "go.etcd.io/bbolt"
)

func persistPayment(db *bolt.DB, account string, source *PaymentSource,
	amount dcrutil.Amount, height uint32, estMaturity uint32) (*Payment, error) {
	pmt := NewPayment(account, source, amount, height, estMaturity)
	err := pmt.Persist(db)
	if err != nil {
		return nil, fmt.Errorf("unable to persist payment: %v", err)
	}
	return pmt, nil
}

func testPayment(t *testing.T) {
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
}

// testPaymentAccessors tests fetchPendingPayments, maturePendingPayments,
// archivedPayments and pendingPaymentsForBlockHash.
func testPaymentAccessors(t *testing.T) {
	height := uint32(10)
	estMaturity := uint32(26)
	zeroHash := chainhash.Hash{0}
	zeroSource := &PaymentSource{
		BlockHash: zeroHash.String(),
		Coinbase:  zeroHash.String(),
	}
	amt, _ := dcrutil.NewAmount(5)
	_, err := persistPayment(db, xID, zeroSource, amt, height+1, estMaturity+1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = persistPayment(db, xID, zeroSource, amt, height+1, estMaturity+1)
	if err != nil {
		t.Fatal(err)
	}

	pmtC, err := persistPayment(db, yID, zeroSource, amt, height, estMaturity)
	if err != nil {
		t.Fatal(err)
	}
	pmtC.PaidOnHeight = estMaturity + 1
	pmtC.TransactionID = zeroHash.String()
	err = pmtC.Update(db)
	if err != nil {
		t.Fatal(err)
	}
	err = pmtC.Archive(db)
	if err != nil {
		t.Fatal(err)
	}

	pmtD, err := persistPayment(db, yID, zeroSource, amt, height, estMaturity)
	if err != nil {
		t.Fatal(err)
	}
	pmtD.PaidOnHeight = estMaturity + 1
	pmtD.TransactionID = chainhash.Hash{0}.String()
	err = pmtD.Update(db)
	if err != nil {
		t.Fatal(err)
	}
	err = pmtD.Archive(db)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure there are two pending payments.
	pmts, err := fetchPendingPayments(db)
	if err != nil {
		t.Fatalf("pendingPayments error: %v", err)
	}

	if len(pmts) != 2 {
		t.Fatalf("expected 2 pending payments, got %d", len(pmts))
	}

	// Ensure there are two archived payments (payment C and D).
	pmts, err = archivedPayments(db)
	if err != nil {
		t.Fatalf("archivedPayments error: %v", err)
	}

	if len(pmts) != 2 {
		t.Fatalf("expected 2 archived payments, got %d", len(pmts))
	}

	// Ensure there are two mature payments at height 28 (payment A and B).
	pmtSet, err := maturePendingPayments(db, 28)
	if err != nil {
		t.Fatalf("maturePendingPayments error: %v", err)
	}

	if len(pmtSet) != 1 {
		t.Fatalf("expected 1 payment set, got %d", len(pmtSet))
	}

	set, ok := pmtSet[zeroSource.BlockHash]
	if !ok {
		t.Fatalf("expected pending payments at height %d to be "+
			"mature at height %d", height+1, 28)
	}

	if len(set) != 2 {
		t.Fatalf("expected 2 mature pending payments from "+
			"height %d, got %d", height+1, len(set))
	}

	// Ensure there are no mature payments at height 27 (payment A and B).
	pmtSet, err = maturePendingPayments(db, 27)
	if err != nil {
		t.Fatalf("maturePendingPayments error: %v", err)
	}

	if len(pmtSet) != 0 {
		t.Fatalf("expected no payment sets, got %d", len(pmtSet))
	}

	// Ensure there are two pending payments for the zero hash.
	count, err := pendingPaymentsForBlockHash(db, zeroSource.BlockHash)
	if err != nil {
		t.Fatalf("pendingPaymentsForBlockHash error: %v", err)
	}

	if count != 2 {
		t.Fatalf("expected 2 mature pending payments with "+
			"block hash %s, got %d", zeroSource.BlockHash, count)
	}
}
