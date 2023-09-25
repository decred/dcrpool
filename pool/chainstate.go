// Copyright (c) 2021-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"

	errs "github.com/decred/dcrpool/errors"
)

const (
	// bufferSize represents the block notification buffer size.
	bufferSize = 128
)

// ChainStateConfig contains all of the configuration values which should be
// provided when creating a new instance of ChainState.
type ChainStateConfig struct {
	// db represents the pool database.
	db Database
	// SoloPool represents the solo pool mining mode.
	SoloPool bool
	// ProcessPayments relays payment signals for processing.
	ProcessPayments func(context.Context, *paymentMsg)
	// GeneratePayments creates payments for participating accounts in pool
	// mining mode based on the configured payment scheme.
	GeneratePayments func(uint32, *PaymentSource, dcrutil.Amount, int64) error
	// GetBlock fetches the block associated with the provided block hash.
	GetBlock func(context.Context, *chainhash.Hash) (*wire.MsgBlock, error)
	// GetBlockConfirmations fetches the block confirmations with the provided
	// block hash.
	GetBlockConfirmations func(context.Context, *chainhash.Hash) (int64, error)
	// SignalCache sends the provided cache update event to the gui cache.
	SignalCache func(event CacheUpdateEvent)
}

// blockNotification wraps a block header notification and a done channel.
type blockNotification struct {
	Header []byte
	Done   chan struct{}
}

// ChainState represents the current state of the chain.
type ChainState struct {
	lastWorkHeight uint32 // update atomically.

	cfg            *ChainStateConfig
	connCh         chan *blockNotification
	discCh         chan *blockNotification
	currentWork    string
	currentWorkMtx sync.RWMutex
}

// NewChainState creates a chain state.
func NewChainState(sCfg *ChainStateConfig) *ChainState {
	return &ChainState{
		cfg:    sCfg,
		connCh: make(chan *blockNotification, bufferSize),
		discCh: make(chan *blockNotification, bufferSize),
	}
}

// fetchLastWorkHeight fetches the last work height.
func (cs *ChainState) fetchLastWorkHeight() uint32 {
	return atomic.LoadUint32(&cs.lastWorkHeight)
}

// setLastWorkHeight updates the last work height.
func (cs *ChainState) setLastWorkHeight(height uint32) {
	atomic.StoreUint32(&cs.lastWorkHeight, height)
}

// setCurrentWork updates the current work.
func (cs *ChainState) setCurrentWork(headerE string) {
	cs.currentWorkMtx.Lock()
	cs.currentWork = headerE
	cs.currentWorkMtx.Unlock()
}

// fetchCurrentWork fetches the current work.
func (cs *ChainState) fetchCurrentWork() string {
	cs.currentWorkMtx.RLock()
	work := cs.currentWork
	cs.currentWorkMtx.RUnlock()
	return work
}

// pruneAcceptedWork removes all accepted work not confirmed as mined work
// with heights less than the provided height.
func (cs *ChainState) pruneAcceptedWork(ctx context.Context, height uint32) error {
	toDelete, err := cs.cfg.db.fetchUnconfirmedWork(height)
	if err != nil {
		return err
	}

	// It is possible to miss mined block confirmations if the pool is
	// restarted, accepted work pruning candidates must be checked to
	// ensure there are not part of the chain before being pruned as a
	// result.
	for _, work := range toDelete {
		hash, err := chainhash.NewHashFromStr(work.BlockHash)
		if err != nil {
			return err
		}
		confs, err := cs.cfg.GetBlockConfirmations(ctx, hash)
		if err != nil {
			// Do not error if the block being pruned cannot be found. It
			// most likely got reorged off the chain.
			if !errors.Is(err, errs.BlockNotFound) {
				return err
			}
		}

		// If the block has no confirmations at the current height,
		// it is an orphan. Prune it.
		if confs <= 0 {
			err = cs.cfg.db.deleteAcceptedWork(work.UUID)
			if err != nil {
				return err
			}

			continue
		}

		// If the block has confirmations mark the accepted work as
		// confirmed.
		work.Confirmed = true
		err = cs.cfg.db.updateAcceptedWork(work)
		if err != nil {
			return err
		}
	}

	return nil
}

// prunePayments removes all spendable payments sourcing from
// orphaned blocks at the provided height.
func (cs *ChainState) prunePayments(ctx context.Context, height uint32) error {
	toDelete, err := cs.cfg.db.fetchPaymentsAtHeight(height)
	if err != nil {
		return err
	}

	for _, payment := range toDelete {
		hash, err := chainhash.NewHashFromStr(payment.Source.BlockHash)
		if err != nil {
			return err
		}

		confs, err := cs.cfg.GetBlockConfirmations(ctx, hash)
		if err != nil {
			return err
		}

		// If the block has no confirmations at the current height,
		// it is an orphan. Delete the payments associated with it.
		if confs <= 0 {
			err = cs.cfg.db.deletePayment(payment.UUID)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// isTreasuryActive checks the provided coinbase transaction if
// the treasury agenda is active.
func isTreasuryActive(tx *wire.MsgTx) bool {
	if tx.Version < wire.TxVersionTreasury {
		return false
	}
	if !standalone.IsCoinBaseTx(tx, true) {
		return false
	}
	return true
}

// handleChainUpdates processes connected and disconnected block
// notifications from the consensus daemon.
//
// This must be run as a goroutine.
func (cs *ChainState) handleChainUpdates(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil

		case msg := <-cs.connCh:
			var header wire.BlockHeader
			err := header.FromBytes(msg.Header)
			if err != nil {
				// Errors generated parsing block notifications should not
				// terminate the chainstate process.
				log.Errorf("unable to create header from bytes: %v", err)
				close(msg.Done)
				continue
			}

			block, err := cs.cfg.GetBlock(ctx, &header.PrevBlock)
			if err != nil {
				// Errors generated fetching blocks of confirmed mined
				// work are curently fatal because payments are
				// sourced from coinbases. The chainstate process will be
				// terminated as a result.
				close(msg.Done)
				return fmt.Errorf("unable to fetch block with hash %x: %w",
					header.PrevBlock, err)
			}

			coinbaseTx := block.Transactions[0]
			treasuryActive := isTreasuryActive(coinbaseTx)

			soloPool := cs.cfg.SoloPool
			if !soloPool {
				go cs.cfg.ProcessPayments(ctx, &paymentMsg{
					CurrentHeight:  header.Height,
					TreasuryActive: treasuryActive,
					Done:           make(chan struct{}),
				})
			}

			// Prune invalidated jobs and accepted work.
			if header.Height > MaxReorgLimit {
				pruneLimit := header.Height - MaxReorgLimit
				err := cs.cfg.db.deleteJobsBeforeHeight(pruneLimit)
				if err != nil {
					// Errors generated pruning invalidated jobs indicate an
					// underlying issue accessing the database. The chainstate
					// process will be terminated as a result.
					close(msg.Done)
					return fmt.Errorf("unable to prune jobs to height %d: %w",
						pruneLimit, err)
				}

				// Prune all hash data not updated in the past ten minutes.
				// A connected client should have updated multiple times
				// by then. Only disconnected miners would not have
				// updated within the timeframe.
				tenMinutesAgo := time.Now().Add(-time.Minute * 10).UnixNano()
				err = cs.cfg.db.pruneHashData(tenMinutesAgo)
				if err != nil {
					// Errors generated pruning invalidated hash rate
					// indicate an underlying issue accessing the
					// database. The chainstate process will be
					// terminated as a result.
					close(msg.Done)
					return fmt.Errorf("unable to prune hash data: %w", err)
				}

				err = cs.pruneAcceptedWork(ctx, pruneLimit)
				if err != nil {
					// Errors generated pruning invalidated accepted
					// work indicate an underlying issue accessing
					// the database. The chainstate process will be
					// terminated as a result.
					close(msg.Done)
					return fmt.Errorf("unable to prune accepted work below "+
						"height #%d: %w", pruneLimit, err)
				}

				err = cs.prunePayments(ctx, header.Height)
				if err != nil {
					// Errors generated pruning invalidated payments
					// indicate an underlying issue accessing the
					// database. The chainstate process will be
					// terminated as a result.
					close(msg.Done)
					return fmt.Errorf("unable to prune orphaned payments at "+
						"height #%d: %w", header.Height, err)
				}
			}

			// Check if the parent of the connected block is an accepted work
			// of the pool.
			parentHeight := header.Height - 1
			parentHash := header.PrevBlock.String()
			parentID := AcceptedWorkID(parentHash, parentHeight)
			work, err := cs.cfg.db.fetchAcceptedWork(parentID)
			if err != nil {
				// If the parent of the connected block is not an accepted
				// work of the pool, ignore it.
				if errors.Is(err, errs.ValueNotFound) {
					log.Tracef("Block #%d (%s) is not an accepted "+
						"work of the pool", parentHeight, parentHash)
					close(msg.Done)
					continue
				}

				// Errors generated, except for a value not found error,
				// looking up accepted work indicates an underlying issue
				// accessing the database. The chainstate process will be
				// terminated as a result.
				close(msg.Done)
				return fmt.Errorf("unable to fetch accepted work for block "+
					"#%d's parent %s : %w", header.Height, parentHash, err)
			}

			// If the parent block is already confirmed as mined by the pool,
			// ignore it.
			if work.Confirmed {
				close(msg.Done)
				continue
			}

			// Update accepted work as confirmed mined.
			work.Confirmed = true
			err = cs.cfg.db.updateAcceptedWork(work)
			if err != nil {
				// Errors generated updating work state indicate an underlying
				// issue accessing the database. The chainstate process will
				// be terminated as a result.
				close(msg.Done)
				return fmt.Errorf("unable to confirm accepted work for block "+
					"%s: %w", header.PrevBlock.String(), err)
			}
			log.Infof("Mined work %s confirmed by connected block #%d (%s)",
				header.PrevBlock.String(), header.Height,
				header.BlockHash().String())

			// Signal the gui cache of the confirmed mined work.
			cs.cfg.SignalCache(Confirmed)

			if !cs.cfg.SoloPool {
				count, err := cs.cfg.db.pendingPaymentsForBlockHash(parentHash)
				if err != nil {
					// Errors generated looking up pending payments
					// indicates an underlying issue accessing the database.
					// The chainstate process will be terminated as a result.
					close(msg.Done)
					return fmt.Errorf("failed to fetch pending payments "+
						"at height #%d: %w", parentHeight, err)
				}

				// If the parent block already has payments generated for it
				// do not generate a new set of payments.
				if count > 0 {
					close(msg.Done)
					continue
				}

				// Generate payments for the confirmed block.
				source := &PaymentSource{
					BlockHash: block.BlockHash().String(),
					Coinbase:  coinbaseTx.TxHash().String(),
				}

				// The coinbase output prior to
				// [DCP0006](https://github.com/decred/dcps/pull/17)
				// activation is at the third index position and at
				// the second index position once DCP0006 is activated.
				amt := dcrutil.Amount(coinbaseTx.TxOut[1].Value)
				if !treasuryActive {
					amt = dcrutil.Amount(coinbaseTx.TxOut[2].Value)
				}

				err = cs.cfg.GeneratePayments(block.Header.Height, source,
					amt, work.CreatedOn)
				if err != nil {
					// Errors generated creating payments are fatal since it is
					// required to distribute payments to participating miners.
					// The chainstate process will be terminated as a result.
					close(msg.Done)
					return err
				}
			}

			close(msg.Done)

		case msg := <-cs.discCh:
			var header wire.BlockHeader
			err := header.FromBytes(msg.Header)
			if err != nil {
				// Errors generated parsing block notifications should not
				// terminate the chainstate process.
				log.Errorf("unable to create header from bytes: %v", err)
				close(msg.Done)
				continue
			}

			// Check if the disconnected block confirms a mined block, if it
			// does unconfirm it.
			parentHeight := header.Height - 1
			parentHash := header.PrevBlock.String()
			parentID := AcceptedWorkID(parentHash, parentHeight)
			confirmedWork, err := cs.cfg.db.fetchAcceptedWork(parentID)
			if err != nil {
				// Errors generated, except for a value not found error,
				// looking up accepted work indicates an underlying issue
				// accessing the database. The chainstate process will be
				// terminated as a result.
				if !errors.Is(err, errs.ValueNotFound) {
					close(msg.Done)
					return fmt.Errorf("unable to fetch accepted work for block "+
						"#%d's parent %s: %w", header.Height, parentHash, err)
				}

				// If the parent of the disconnected block is not an accepted
				// work of the pool, ignore it.
			}

			if confirmedWork != nil {
				confirmedWork.Confirmed = false
				err = cs.cfg.db.updateAcceptedWork(confirmedWork)
				if err != nil {
					// Errors generated updating work state indicate an underlying
					// issue accessing the database. The chainstate process will
					// be terminated as a result.
					close(msg.Done)
					return fmt.Errorf("unable to unconfirm accepted work for "+
						"block %s: %w", parentHash, err)
				}

				log.Infof("Mined work %s unconfirmed via disconnected "+
					"block #%d", parentHash, header.Height)
			}

			// If the disconnected block is an accepted work of the pool
			// ensure it is not confirmed mined.
			blockHash := header.BlockHash().String()
			id := AcceptedWorkID(blockHash, header.Height)
			work, err := cs.cfg.db.fetchAcceptedWork(id)
			if err != nil {
				// If the disconnected block is not an accepted
				// work of the pool, ignore it.
				if errors.Is(err, errs.ValueNotFound) {
					close(msg.Done)
					continue
				}

				// Errors generated, except for a value not found error,
				// looking up accepted work indicates an underlying issue
				// accessing the database. The chainstate process will be
				// terminated as a result.
				close(msg.Done)
				return fmt.Errorf("unable to fetch accepted work for block "+
					"#%d (hash %s): %w", header.Height, blockHash, err)
			}

			work.Confirmed = false
			err = cs.cfg.db.updateAcceptedWork(work)
			if err != nil {
				// Errors generated updating work state indicate an underlying
				// issue accessing the database. The chainstate process will
				// be terminated as a result.
				close(msg.Done)
				return fmt.Errorf("unable to unconfirm mined work at height "+
					"#%d: %w", header.Height, err)
			}

			log.Infof("Disconnected mined work %s at height #%d",
				blockHash, header.Height)

			// Signal the gui cache of the unconfirmed (due to a reorg)
			// mined work.
			cs.cfg.SignalCache(Unconfirmed)

			close(msg.Done)
		}
	}
}
