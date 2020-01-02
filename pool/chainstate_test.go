package pool

import (
	"context"
	"encoding/hex"
	"sync"
	"testing"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/wire"
)

func testChainState(t *testing.T, db *bolt.DB) {
	ctx, cancel := context.WithCancel(context.Background())
	var minedHeader wire.BlockHeader
	var confHeader wire.BlockHeader
	cCfg := &ChainStateConfig{
		DB:       db,
		SoloPool: false,
		PayDividends: func(uint32) error {
			return nil
		},
		GeneratePayments: func(uint32, dcrutil.Amount) error {
			return nil
		},
		GetBlock: func(*chainhash.Hash) (*wire.MsgBlock, error) {
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
		},
		Cancel: cancel,
		HubWg:  new(sync.WaitGroup),
	}

	cs := NewChainState(cCfg)
	cCfg.HubWg.Add(1)
	go cs.handleChainUpdates(ctx)

	// Create the accepted work to be confirmed.
	work := NewAcceptedWork(
		"00007979602e13db87f6c760bbf27c137f4112b9e1988724bd245fb0bb7d1283",
		"00006fb4ee4609e90196cfa41df2f1129a64553f935f21e6940b38e7e26e7dff",
		42, xID, CPU)
	err := work.Create(cs.cfg.DB)
	if err != nil {
		t.Fatalf("unable to persist accepted work %v", err)
	}

	// Create associated job of the work to be confirmed.
	workE := "07000000ff7d6ee2e7380b94e6215f933f55649a12f1f21da4cf" +
		"9601e90946eeb46f000066f27e7f98656bc19195a0a6d3a93d0d774b2e5" +
		"83f49f20f6fef11b38443e21a05bad23ac3f14278f0ad74a86ce08ca44d" +
		"05e0e2b0cd3bc91066904c311f482e01000000000000000000000000000" +
		"0004fa83b20204e0000000000002a000000a50300004348fa5d00000000" +
		"00000000000000000000000000000000000000000000000000000000000" +
		"00000000000008000000100000000000005a0"
	job, err := NewJob(workE, 42)
	if err != nil {
		t.Fatalf("unable to create job %v", err)
	}
	err = job.Create(cs.cfg.DB)
	if err != nil {
		log.Errorf("failed to persist job %v", err)
		return
	}

	// Create mined work header.
	headerE := "07000000ff7d6ee2e7380b94e6215f933f55649a12f1f21da4cf" +
		"9601e90946eeb46f000066f27e7f98656bc19195a0a6d3a93d0d774b2e5" +
		"83f49f20f6fef11b38443e21a05bad23ac3f14278f0ad74a86ce08ca44d" +
		"05e0e2b0cd3bc91066904c311f482e01000000000000000000000000000" +
		"0004fa83b20204e0000000000002a000000a50300004348fa5db0350100" +
		"e7879320000000000000000000000000000000000000000000000000000" +
		"0000000000000"
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

	// Create confirmation block header.
	headerE = "0700000083127dbbb05f24bd248798e1b912417f137cf2bb60c7" +
		"f687db132e60797900007d79305d0f130f1371fe8e6a7d8aed721e969d4" +
		"5e94ecbffe229c85e3fea8d2b8aba150b44486aa58f4c01a574e6a6c865" +
		"766b3d3554fcc31d7061741e424158010000000000000000000a0000000" +
		"0004fa83b20204e0000000000002b0000003a0f00004548fa5d70230000" +
		"e78793200000000000000000000000000000000000000000000000000000" +
		"000000000000"
	headerB, err = hex.DecodeString(headerE)
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
		Done:   make(chan bool),
	}
	cs.connCh <- minedMsg
	<-minedMsg.Done
	confMsg := &blockNotification{
		Header: confHeaderB,
		Done:   make(chan bool),
	}
	cs.connCh <- confMsg
	<-confMsg.Done

	// Ensure the accepted work is now confirmed mined.
	confirmedWork, err := FetchAcceptedWork(cs.cfg.DB, []byte(work.UUID))
	if err != nil {
		t.Fatalf("unable to confirm accepted work: %v", err)
	}
	if !confirmedWork.Confirmed {
		t.Fatalf("expected accepted work to be confirmed " +
			"after chain notifications")
	}

	discConfMsg := &blockNotification{
		Header: confHeaderB,
		Done:   make(chan bool),
	}
	cs.discCh <- discConfMsg
	<-discConfMsg.Done
	discMinedMsg := &blockNotification{
		Header: minedHeaderB,
		Done:   make(chan bool),
	}
	cs.discCh <- discMinedMsg
	<-discMinedMsg.Done

	// Ensure the confirmed mined work is now removed.
	_, err = FetchAcceptedWork(cs.cfg.DB, []byte(work.UUID))
	if err == nil {
		t.Fatalf("expected a value not found error")
	}

	// Ensure the last work height can be updated.
	initialLastWorkHeight := cs.fetchLastWorkHeight()
	updatedLastWorkHeight := uint32(100)
	cs.setLastWorkHeight(updatedLastWorkHeight)
	lastWorkHeight := cs.fetchLastWorkHeight()
	if lastWorkHeight == initialLastWorkHeight {
		t.Fatalf("expected last work height to be %d, got %d",
			updatedLastWorkHeight, lastWorkHeight)
	}

	// Enwsure the current work can be updated.
	initialCurrentWork := cs.fetchCurrentWork()
	updatedCurrentWork := headerE
	cs.setCurrentWork(updatedCurrentWork)
	currentWork := cs.fetchCurrentWork()
	if currentWork == initialCurrentWork {
		t.Fatalf("expected current work height to be %s, got %s",
			updatedCurrentWork, currentWork)
	}

	// Switch the

	cancel()
	cs.cfg.HubWg.Wait()
}
