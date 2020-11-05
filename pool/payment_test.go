// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"

	"github.com/decred/dcrpool/errors"
)

func testPayment(t *testing.T) {
	height := uint32(10)
	estMaturity := uint32(26)
	zeroSource := &PaymentSource{
		BlockHash: chainhash.Hash{0}.String(),
		Coinbase:  chainhash.Hash{0}.String(),
	}

	// Create some valid payments.
	amt, _ := dcrutil.NewAmount(5)
	pmtA := NewPayment(xID, zeroSource, amt, height, estMaturity)
	err := db.PersistPayment(pmtA)
	if err != nil {
		t.Fatal(err)
	}

	pmtB := NewPayment(xID, zeroSource, amt, height, estMaturity)
	err = db.PersistPayment(pmtB)
	if err != nil {
		t.Fatal(err)
	}

	pmtC := NewPayment(yID, zeroSource, amt, height, estMaturity)
	err = db.PersistPayment(pmtC)
	if err != nil {
		t.Fatal(err)
	}

	// Fetch a payment using its id.
	fetchedPayment, err := db.fetchPayment(pmtA.UUID)
	if err != nil {
		t.Fatalf("fetchPayment err: %v", err)
	}

	// Ensure fetched values match persisted values.
	if fetchedPayment.Account != pmtA.Account {
		t.Fatalf("expected %v as fetched payment account, got %v",
			pmtA.Account, fetchedPayment.Account)
	}

	if fetchedPayment.EstimatedMaturity != pmtA.EstimatedMaturity {
		t.Fatalf("expected %v as fetched payment est maturity, got %v",
			pmtA.EstimatedMaturity, fetchedPayment.EstimatedMaturity)
	}

	if fetchedPayment.Height != pmtA.Height {
		t.Fatalf("expected %v as fetched payment height, got %v",
			pmtA.Height, fetchedPayment.Height)
	}

	if fetchedPayment.Amount != pmtA.Amount {
		t.Fatalf("expected %v as fetched payment amount, got %v",
			pmtA.Amount, fetchedPayment.Amount)
	}

	if fetchedPayment.CreatedOn != pmtA.CreatedOn {
		t.Fatalf("expected %v as fetched payment createdon, got %v",
			pmtA.CreatedOn, fetchedPayment.CreatedOn)
	}

	if fetchedPayment.PaidOnHeight != pmtA.PaidOnHeight {
		t.Fatalf("expected %v as fetched payment paidonheight, got %v",
			pmtA.PaidOnHeight, fetchedPayment.PaidOnHeight)
	}

	if fetchedPayment.TransactionID != pmtA.TransactionID {
		t.Fatalf("expected %v as fetched payment transactionid, got %v",
			pmtA.TransactionID, fetchedPayment.TransactionID)
	}

	if fetchedPayment.Source.Coinbase != pmtA.Source.Coinbase {
		t.Fatalf("expected %v as fetched payment source coinbase, got %v",
			pmtA.Source.Coinbase, fetchedPayment.Source.Coinbase)
	}

	if fetchedPayment.Source.BlockHash != pmtA.Source.BlockHash {
		t.Fatalf("expected %v as fetched payment source blockhash, got %v",
			pmtA.Source.BlockHash, fetchedPayment.Source.BlockHash)
	}

	// Ensure payments can be updated.
	txid := chainhash.Hash{0}.String()
	pmtB.TransactionID = txid
	err = db.updatePayment(pmtB)
	if err != nil {
		t.Fatalf("payment update err: %v", err)
	}

	fetchedPayment, err = db.fetchPayment(pmtB.UUID)
	if err != nil {
		t.Fatalf("fetchPayment err: %v", err)
	}

	if fetchedPayment.TransactionID != txid {
		t.Fatalf("expected payment with tx id %s, got %s",
			txid, fetchedPayment.TransactionID)
	}

	// Persist payment B as an archived payment.
	pmtB.PaidOnHeight = estMaturity + 1
	err = db.ArchivePayment(pmtB)
	if err != nil {
		t.Fatalf("payment delete error: %v", err)
	}

	// Ensure the payment B was archived.
	_, err = db.fetchPayment(pmtB.UUID)
	if !errors.Is(err, errors.ValueNotFound) {
		t.Fatalf("expected value not found error, got %v", err)
	}

	// Delete payment C.
	err = db.deletePayment(pmtC.UUID)
	if err != nil {
		t.Fatalf("payment delete error: %v", err)
	}

	// Ensure the payment C was deleted.
	fetchedPayment, err = db.fetchPayment(pmtC.UUID)
	if !errors.Is(err, errors.ValueNotFound) {
		t.Fatalf("expected value not found error, got %v", err)
	}

	if fetchedPayment != nil {
		t.Fatal("expected a nil payment")
	}

	// Deleting a payment which does not exist should not return an error.
	err = db.deletePayment(pmtC.UUID)
	if err != nil {
		t.Fatalf("payment delete error: %v", err)
	}

	// Updating a payment which does not exist should not return an error.
	err = db.updatePayment(pmtC)
	if err != nil {
		t.Fatalf("payment update error: %v", err)
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
	pmtA := NewPayment(xID, zeroSource, amt, height+1, estMaturity+1)
	err := db.PersistPayment(pmtA)
	if err != nil {
		t.Fatal(err)
	}

	pmtB := NewPayment(xID, zeroSource, amt, height+1, estMaturity+1)
	err = db.PersistPayment(pmtB)
	if err != nil {
		t.Fatal(err)
	}

	pmtC := NewPayment(yID, zeroSource, amt, height, estMaturity)
	pmtC.PaidOnHeight = estMaturity + 1
	pmtC.TransactionID = zeroHash.String()
	err = db.PersistPayment(pmtC)
	if err != nil {
		t.Fatal(err)
	}

	err = db.ArchivePayment(pmtC)
	if err != nil {
		t.Fatal(err)
	}

	pmtD := NewPayment(yID, zeroSource, amt, height, estMaturity)
	pmtD.PaidOnHeight = estMaturity + 1
	pmtD.TransactionID = chainhash.Hash{0}.String()
	err = db.PersistPayment(pmtD)
	if err != nil {
		t.Fatal(err)
	}

	err = db.ArchivePayment(pmtD)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure there are two pending payments.
	pmts, err := db.fetchPendingPayments()
	if err != nil {
		t.Fatalf("pendingPayments error: %v", err)
	}

	if len(pmts) != 2 {
		t.Fatalf("expected 2 pending payments, got %d", len(pmts))
	}

	// Ensure there are two archived payments (payment C and D).
	pmts, err = db.archivedPayments()
	if err != nil {
		t.Fatalf("archivedPayments error: %v", err)
	}

	if len(pmts) != 2 {
		t.Fatalf("expected 2 archived payments, got %d", len(pmts))
	}

	// Ensure there are two mature payments at height 28 (payment A and B).
	pmtSet, err := db.maturePendingPayments(28)
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
	pmtSet, err = db.maturePendingPayments(27)
	if err != nil {
		t.Fatalf("maturePendingPayments error: %v", err)
	}

	if len(pmtSet) != 0 {
		t.Fatalf("expected no payment sets, got %d", len(pmtSet))
	}

	// Ensure there are two pending payments for the zero hash.
	count, err := db.pendingPaymentsForBlockHash(zeroSource.BlockHash)
	if err != nil {
		t.Fatalf("pendingPaymentsForBlockHash error: %v", err)
	}

	if count != 2 {
		t.Fatalf("expected 2 mature pending payments with "+
			"block hash %s, got %d", zeroSource.BlockHash, count)
	}
}
