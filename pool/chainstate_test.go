// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"

	errs "github.com/decred/dcrpool/errors"
)

func testChainState(t *testing.T) {
	var minedHeader wire.BlockHeader

	// Create mined work header.
	const headerE = "07000000ff7d6ee2e7380b94e6215f933f55649a12f1f21da4cf9601" +
		"e90946eeb46f000066f27e7f98656bc19195a0a6d3a93d0d774b2e583f49f20f6fef" +
		"11b38443e21a05bad23ac3f14278f0ad74a86ce08ca44d05e0e2b0cd3bc91066904c" +
		"311f482e010000000000000000000000000000004fa83b20204e0000000000002a00" +
		"0000a50300004348fa5db0350100e787932000000000000000000000000000000000" +
		"00000000000000000000000000000000"
	headerB, err := hex.DecodeString(headerE)
	if err != nil {
		t.Fatalf("unexpected encoding error %v", err)
	}
	err = minedHeader.FromBytes(headerB)
	if err != nil {
		t.Fatalf("unexpected deserialization error: %v", err)
	}
	minedHeaderB, err := minedHeader.Bytes()
	if err != nil {
		t.Fatalf("unexpected serialization error: %v", err)
	}

	processPayments := func(*paymentMsg) {}
	generatePayments := func(uint32, *PaymentSource, dcrutil.Amount, int64) error {
		return nil
	}
	getBlock := func(context.Context, *chainhash.Hash) (*wire.MsgBlock, error) {
		// Return a fake block.
		coinbase := wire.NewMsgTx()
		coinbase.AddTxOut(wire.NewTxOut(0, []byte{}))
		coinbase.AddTxOut(wire.NewTxOut(1, []byte{}))
		coinbase.AddTxOut(wire.NewTxOut(100, []byte{}))
		txs := make([]*wire.MsgTx, 1)
		txs[0] = coinbase
		block := &wire.MsgBlock{
			Header:       minedHeader,
			Transactions: txs,
		}
		return block, nil
	}

	getBlockConfirmations := func(context.Context, *chainhash.Hash) (int64, error) {
		return -1, nil
	}

	signalCache := func(_ CacheUpdateEvent) {
		// Do nothing.
	}

	ctx, cancel := context.WithCancel(context.Background())
	var confHeader wire.BlockHeader
	cCfg := &ChainStateConfig{
		db:                    db,
		SoloPool:              false,
		ProcessPayments:       processPayments,
		GeneratePayments:      generatePayments,
		GetBlock:              getBlock,
		GetBlockConfirmations: getBlockConfirmations,
		SignalCache:           signalCache,
		Cancel:                cancel,
	}

	cs := NewChainState(cCfg)

	// Test pruneAcceptedWork.
	workA := NewAcceptedWork(
		"00000000000000001e2065a7248a9b4d3886fe3ca3128eebedddaf35fb26e58c",
		"000000000000000007301a21efa98033e06f7eba836990394fff9f765f1556b1",
		396692, yID, "dr3")
	workA.Confirmed = true
	err = db.persistAcceptedWork(workA)
	if err != nil {
		t.Fatal(err)
	}

	workB := NewAcceptedWork(
		"000000000000000025aa4a7ba8c3ece4608376bf84a82ec7e025991460097198",
		"00000000000000001e2065a7248a9b4d3886fe3ca3128eebedddaf35fb26e58c",
		396693, xID, "dr5")
	err = db.persistAcceptedWork(workB)
	if err != nil {
		t.Fatal(err)
	}

	workC := NewAcceptedWork(
		zeroHash.String(),
		"00000000000000001e2065a7248a9b4d3886fe3ca3128eebedddaf35fb26e58c",
		396694, xID, "dr5")
	err = db.persistAcceptedWork(workC)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure mined work cannot be pruned.
	err = cs.pruneAcceptedWork(ctx, workB.Height+1)
	if err != nil {
		t.Fatalf("pruneAcceptedWork error: %v", err)
	}

	// Ensure pruning work associated with a reorged or non-existent
	// block does not error.
	cCfg.GetBlockConfirmations = func(context.Context, *chainhash.Hash) (int64, error) {
		blockNotFoundErr := &dcrjson.RPCError{
			Code:    dcrjson.ErrRPCBlockNotFound,
			Message: fmt.Sprintf("Block not found: %v", zeroHash),
		}
		desc := fmt.Sprintf("unable to fetch block confirmations: %v",
			blockNotFoundErr)
		return -1, errs.PoolError(errs.BlockNotFound, desc)
	}

	err = cs.pruneAcceptedWork(ctx, workC.Height+1)
	if err != nil {
		t.Fatalf("unexpected pruneAcceptedWork error: %v", err)
	}

	cCfg.GetBlockConfirmations = func(context.Context, *chainhash.Hash) (int64, error) {
		return -1, nil
	}

	// Ensure work A did not get pruned but work B did.
	_, err = db.fetchAcceptedWork(workA.UUID)
	if err != nil {
		t.Fatalf("expected a valid accepted work, got: %v", err)
	}
	_, err = db.fetchAcceptedWork(workB.UUID)
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected value found error, got %v", err)
	}

	// Delete work A.
	err = db.deleteAcceptedWork(workA.UUID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test prunePayments.
	cs.cfg.GetBlock = func(ctx context.Context, hash *chainhash.Hash) (*wire.MsgBlock, error) {
		return nil, &dcrjson.RPCError{
			Code:    dcrjson.ErrRPCBlockNotFound,
			Message: fmt.Sprintf("no block found with hash: %v", hash.String()),
		}
	}
	height := uint32(10)
	estMaturity := uint32(26)
	amt, _ := dcrutil.NewAmount(5)
	zeroSource := &PaymentSource{
		BlockHash: chainhash.Hash{0}.String(),
		Coinbase:  chainhash.Hash{0}.String(),
	}
	paymentA := NewPayment(xID, zeroSource, amt, height, estMaturity)
	err = db.PersistPayment(paymentA)
	if err != nil {
		t.Fatal(err)
	}

	paymentB := NewPayment(yID, zeroSource, amt, height+1, estMaturity+1)
	err = db.PersistPayment(paymentB)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure payment A and B do not get pruned at height 27.
	err = cs.prunePayments(ctx, 27)
	if err != nil {
		t.Fatalf("prunePayments error: %v", err)
	}

	_, err = db.fetchPayment(paymentA.UUID)
	if err != nil {
		t.Fatalf("unexpected error fetching payment A: %v", err)
	}

	_, err = db.fetchPayment(paymentB.UUID)
	if err != nil {
		t.Fatalf("unexpected error fetching payment B: %v", err)
	}

	// Ensure payment A gets pruned with payment B remaining at height 28.
	err = cs.prunePayments(ctx, 28)
	if err != nil {
		t.Fatalf("prunePayments error: %v", err)
	}

	_, err = db.fetchPayment(paymentA.UUID)
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected value found error, got %v", err)
	}

	_, err = db.fetchPayment(paymentB.UUID)
	if err != nil {
		t.Fatalf("unexpected error fetching payment B: %v", err)
	}

	// Ensure payment B gets pruned at height 29.
	err = cs.prunePayments(ctx, 29)
	if err != nil {
		t.Fatalf("prunePayments error: %v", err)
	}

	_, err = db.fetchPayment(paymentB.UUID)
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected value found error, got %v", err)
	}

	cs.cfg.GetBlock = getBlock

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		cs.handleChainUpdates(ctx)
		wg.Done()
	}()

	// Create the accepted work to be confirmed.
	work := NewAcceptedWork(
		"00007979602e13db87f6c760bbf27c137f4112b9e1988724bd245fb0bb7d1283",
		"00006fb4ee4609e90196cfa41df2f1129a64553f935f21e6940b38e7e26e7dff",
		42, xID, CPU)
	err = cs.cfg.db.persistAcceptedWork(work)
	if err != nil {
		t.Fatalf("unable to persist accepted work %v", err)
	}

	// Create associated job of the work to be confirmed.
	const workE = "07000000ff7d6ee2e7380b94e6215f933f55649a12f1f21da4cf9601e9" +
		"0946eeb46f000066f27e7f98656bc19195a0a6d3a93d0d774b2e583f49f20f6fef11" +
		"b38443e21a05bad23ac3f14278f0ad74a86ce08ca44d05e0e2b0cd3bc91066904c31" +
		"1f482e010000000000000000000000000000004fa83b20204e0000000000002a0000" +
		"00a50300004348fa5d00000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000008000000100000000000005a0"
	job := NewJob(workE, 42)
	err = cs.cfg.db.persistJob(job)
	if err != nil {
		log.Errorf("failed to persist job %v", err)
		return
	}

	// Ensure a malformed connected block does not terminate the chain
	// state process.
	const headerME = "0700000083127dbbb05f24bd248798e1b912417f137cf2bb60c7f68" +
		"7db132e60797900007d79305d0f130f1371fe8e6a7d8aed721e969d45e94ecbffe22" +
		"9c85e3fea8d2b8aba150b44486aa58f4c01a574e6a6c865766b3d3554fcc31d70617" +
		"41e424158010000000000000000000a00000000004fa83b20204e0000000000002b0" +
		"000003a0f00004548fa5d70230000e78793200000000000000000000000000000000" +
		"0000000000000000000000000000000"
	headerMB, err := hex.DecodeString(headerME)
	if err != nil {
		t.Fatalf("unexpected encoding error %v", err)
	}

	malformedMsg := &blockNotification{
		Header: headerMB,
		Done:   make(chan struct{}),
	}
	cs.connCh <- malformedMsg
	<-malformedMsg.Done

	// Create confirmation block header.
	const headerE2 = "0700000083127dbbb05f24bd248798e1b912417f137cf2bb60c7f68" +
		"7db132e60797900007d79305d0f130f1371fe8e6a7d8aed721e969d45e94ecbffe22" +
		"9c85e3fea8d2b8aba150b44486aa58f4c01a574e6a6c865766b3d3554fcc31d70617" +
		"41e424158010000000000000000000a00000000004fa83b20204e0000000000002b0" +
		"000003a0f00004548fa5d70230000e78793200000000000000000000000000000000" +
		"000000000000000000000000000000000"
	headerB, err = hex.DecodeString(headerE2)
	if err != nil {
		t.Fatalf("unexpected encoding error %v", err)
	}
	err = confHeader.FromBytes(headerB)
	if err != nil {
		t.Fatalf("unexpected deserialization error: %v", err)
	}
	confHeaderB, err := confHeader.Bytes()
	if err != nil {
		t.Fatalf("unexpected serialization error: %v", err)
	}

	minedMsg := &blockNotification{
		Header: minedHeaderB,
		Done:   make(chan struct{}),
	}
	cs.connCh <- minedMsg
	<-minedMsg.Done
	confMsg := &blockNotification{
		Header: confHeaderB,
		Done:   make(chan struct{}),
	}
	cs.connCh <- confMsg
	<-confMsg.Done

	// Ensure the accepted work is now confirmed mined.
	confirmedWork, err := cs.cfg.db.fetchAcceptedWork(work.UUID)
	if err != nil {
		t.Fatalf("unable to confirm accepted work: %v", err)
	}
	if !confirmedWork.Confirmed {
		t.Fatalf("expected accepted work to be confirmed " +
			"after chain notifications")
	}

	discConfMsg := &blockNotification{
		Header: confHeaderB,
		Done:   make(chan struct{}),
	}
	cs.discCh <- discConfMsg
	<-discConfMsg.Done
	discMinedMsg := &blockNotification{
		Header: minedHeaderB,
		Done:   make(chan struct{}),
	}
	cs.discCh <- discMinedMsg
	<-discMinedMsg.Done

	// Ensure the mined work is no longer confirmed mined.
	discMinedWork, err := cs.cfg.db.fetchAcceptedWork(work.UUID)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if discMinedWork.Confirmed {
		t.Fatalf("disconnected mined work a height #%d should "+
			"be unconfirmed", discMinedWork.Height)
	}

	// Ensure a malformed disconnected block does not terminate the chain state
	// process.
	malformedMsg = &blockNotification{
		Header: headerMB,
		Done:   make(chan struct{}),
	}
	cs.discCh <- malformedMsg
	<-malformedMsg.Done

	confMsg = &blockNotification{
		Header: confHeaderB,
		Done:   make(chan struct{}),
	}
	cs.connCh <- confMsg
	<-confMsg.Done
	discConfMsg = &blockNotification{
		Header: confHeaderB,
		Done:   make(chan struct{}),
	}
	cs.discCh <- discConfMsg
	<-discConfMsg.Done

	// Ensure the last work height can be updated.
	initialLastWorkHeight := cs.fetchLastWorkHeight()
	updatedLastWorkHeight := uint32(100)
	cs.setLastWorkHeight(updatedLastWorkHeight)
	lastWorkHeight := cs.fetchLastWorkHeight()
	if lastWorkHeight == initialLastWorkHeight {
		t.Fatalf("expected last work height to be %d, got %d",
			updatedLastWorkHeight, lastWorkHeight)
	}

	// Ensure the current work can be updated.
	initialCurrentWork := cs.fetchCurrentWork()
	updatedCurrentWork := headerE2
	cs.setCurrentWork(updatedCurrentWork)
	currentWork := cs.fetchCurrentWork()
	if currentWork == initialCurrentWork {
		t.Fatalf("expected current work height to be %s, got %s",
			updatedCurrentWork, currentWork)
	}

	cancel()
	wg.Wait()
}
