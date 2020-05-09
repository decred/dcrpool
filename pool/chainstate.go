package pool

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/wire"
	bolt "go.etcd.io/bbolt"
)

var (
	// bufferSize represents the block notification buffer size.
	bufferSize = 128
)

type ChainStateConfig struct {
	// DB represents the pool database.
	DB *bolt.DB
	// SoloPool represents the solo pool mining mode.
	SoloPool bool
	// PayDividends pays mature mining rewards to participating accounts.
	PayDividends func(uint32) error
	// GeneratePayments creates payments for participating accounts in pool
	// mining mode based on the configured payment scheme.
	GeneratePayments func(uint32, *PaymentSource, dcrutil.Amount) error
	// GetBlock fetches the block associated with the provided block hash.
	GetBlock func(*chainhash.Hash) (*wire.MsgBlock, error)
	// GetBlockConfirmations fetches the block confirmations with the provided
	// block hash.
	GetBlockConfirmations func(*chainhash.Hash) (int64, error)
	// PendingPaymentsAtHeight fetches all pending payments at
	// the provided height.
	PendingPaymentsAtHeight func(uint32) ([]*Payment, error)
	// PendingPaymentsForBlockHash returns the  number of pending payments
	// with the provided block hash as their source.
	PendingPaymentsForBlockHash func(blockHash string) (uint32, error)
	// Cancel represents the pool's context cancellation function.
	Cancel context.CancelFunc
	// SignalCache sends the provided cache update event to the gui cache.
	SignalCache func(event CacheUpdateEvent)
	// HubWg represents the hub's waitgroup.
	HubWg *sync.WaitGroup
}

// blockNotification wraps a block header notification and a done channel.
type blockNotification struct {
	Header []byte
	Done   chan bool
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

// NewChainState creates a a chain state.
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
func (cs *ChainState) pruneAcceptedWork(height uint32) error {
	err := cs.cfg.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchWorkBucket(tx)
		if err != nil {
			return err
		}

		toDelete := make(map[string]*AcceptedWork)
		heightBE := heightToBigEndianBytes(height)
		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			height, err := hex.DecodeString(string(k[:8]))
			if err != nil {
				return err
			}

			if bytes.Compare(heightBE, height) > 0 {
				var work AcceptedWork
				err := json.Unmarshal(v, &work)
				if err != nil {
					return err
				}

				if !work.Confirmed {
					toDelete[string(k)] = &work
				}
			}
		}

		// It is possible to miss mined block confirmations if the pool is
		// restarted, accepted work pruning candidates must be checked to
		// ensure there are not part of the chain before being pruned as a
		// result.
		for k, work := range toDelete {
			hash, err := chainhash.NewHashFromStr(work.BlockHash)
			if err != nil {
				return err
			}
			confs, err := cs.cfg.GetBlockConfirmations(hash)
			if err != nil {
				return err
			}

			// If the block has no confirmations at the current height,
			// it is an orphan. Prune it.
			if confs <= 0 {
				err := bkt.Delete([]byte(k))
				if err != nil {
					return err
				}

				continue
			}

			// Mark the accepted work as confirmed if it has confirmations.
			work.Confirmed = true
			err = work.Update(cs.cfg.DB)
			if err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

// prunePayments removes all spendabe payments sourcing from
// orphaned blocks at the provided height.
func (cs *ChainState) prunePayments(height uint32) error {
	err := cs.cfg.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchPaymentBucket(tx)
		if err != nil {
			return err
		}

		toDelete := make(map[string]*chainhash.Hash)
		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				return err
			}

			if payment.PaidOnHeight == 0 {
				// If a payment is spendable but does not get processed it
				// becomes eligible for pruning.
				spendableHeight := payment.EstimatedMaturity + 1
				if height > spendableHeight {
					hash, err := chainhash.NewHashFromStr(payment.Source.BlockHash)
					if err != nil {
						return err
					}

					toDelete[string(k)] = hash
				}
			}

		}

		for k, hash := range toDelete {
			confs, err := cs.cfg.GetBlockConfirmations(hash)
			if err != nil {
				return err
			}

			// If the block has no confirmations at the current height,
			// it is an orphan. Delete the payments associated with it.
			if confs <= 0 {
				err := bkt.Delete([]byte(k))
				if err != nil {
					return err
				}
			}
		}

		return nil
	})

	return err
}

// pruneJobs removes all jobs with heights less than the provided height.
func (cs *ChainState) pruneJobs(height uint32) error {
	heightBE := heightToBigEndianBytes(height)
	err := cs.cfg.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchJobBucket(tx)
		if err != nil {
			return err
		}

		toDelete := [][]byte{}
		c := bkt.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			height, err := hex.DecodeString(string(k[:8]))
			if err != nil {
				return err
			}

			if bytes.Compare(height, heightBE) < 0 {
				toDelete = append(toDelete, k)
			}
		}

		for _, entry := range toDelete {
			err := bkt.Delete(entry)
			if err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

// handleChainUpdates processes connected and disconnected block
// notifications from the consensus daemon.
func (cs *ChainState) handleChainUpdates(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			close(cs.discCh)
			close(cs.connCh)
			cs.cfg.HubWg.Done()
			return

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

			// Prune invalidated jobs and accepted work.
			if header.Height > MaxReorgLimit {
				pruneLimit := header.Height - MaxReorgLimit
				err := cs.pruneJobs(pruneLimit)
				if err != nil {
					// Errors generated pruning invalidated jobs indicate an
					// underlying issue accessing the database. The chainstate
					// process will be terminated as a result.
					log.Errorf("unable to prune jobs to height %d: %v",
						pruneLimit, err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}

				err = cs.pruneAcceptedWork(pruneLimit)
				if err != nil {
					// Errors generated pruning invalidated accepted
					// work indicate an underlying issue accessing
					// the database. The chainstate process will be
					// terminated as a result.
					log.Errorf("unable to prune accepted work below "+
						"height #%d: %v", pruneLimit, err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}

				err = cs.prunePayments(header.Height)
				if err != nil {
					// Errors generated pruning invalidated payments
					// indicate an underlying issue accessing the
					// database. The chainstate process will be
					// terminated as a result.
					log.Errorf("unable to prune orphaned payments at "+
						"height #%d: %v", header.Height, err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}
			}

			// Check if the parent of the connected block is an accepted work
			// of the pool.
			parentHeight := header.Height - 1
			parentHash := header.PrevBlock.String()
			parentID := AcceptedWorkID(parentHash, parentHeight)
			work, err := FetchAcceptedWork(cs.cfg.DB, parentID)
			if err != nil {
				// If the parent of the connected block is not an accepted
				// work of the the pool, ignore it.
				if IsError(err, ErrValueNotFound) {
					log.Tracef("Block %d (%s) not an accepted work of the pool",
						parentHeight, parentHash)
					close(msg.Done)
					continue
				}

				// Errors generated, except for a value not found error,
				// looking up accepted work indicates an underlying issue
				// accessing the database. The chainstate process will be
				// terminated as a result.
				log.Errorf("unable to fetch accepted work for block #%d's "+
					"parent %s : %v", header.Height, parentHash, err)
				close(msg.Done)
				cs.cfg.Cancel()
				continue
			}

			//  If the parent block is already confirmed as mined by the pool,
			// ignore it.
			if work.Confirmed {
				close(msg.Done)
				continue
			}

			if !cs.cfg.SoloPool {
				count, err := cs.cfg.PendingPaymentsForBlockHash(parentHash)
				if err != nil {
					// Errors generated looking up pending payments
					// indicates an underlying issue accessing the database.
					// The chainstate process will be terminated as a result.
					log.Errorf("failed to fetch pending payments "+
						"at height #%d: %v", parentHeight, err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}

				// If the parent block already has payments generated for it
				// do not generate a new set of payments.
				if count > 0 {
					close(msg.Done)
					continue
				}

				// TODO: look into keeping track of payment processing for
				// confirmed mined work to facilitate recovery when the
				// getblock calls err.
				block, err := cs.cfg.GetBlock(&header.PrevBlock)
				if err != nil {
					// Errors generated fetching blocks of confirmed mined
					// work are curently fatal because payments are
					// sourced from coinbases. The chainstate process will be
					// terminated as a result.
					log.Errorf("unable to fetch block with hash %x: %v",
						header.PrevBlock, err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}

				// Generate payments for the confirmed block.
				source := &PaymentSource{
					BlockHash: block.BlockHash().String(),
					Coinbase:  block.Transactions[0].TxHash().String(),
				}
				amt := dcrutil.Amount(block.Transactions[0].TxOut[2].Value)
				err = cs.cfg.GeneratePayments(block.Header.Height, source, amt)
				if err != nil {
					// Errors generated creating payments are fatal since it is
					// required to distribute payments to participating miners.
					// The chainstate process will be terminated as a result.
					log.Errorf("unable to generate payments: %v", err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}

				// Update accepted work as confirmed mined.
				work.Confirmed = true
				err = work.Update(cs.cfg.DB)
				if err != nil {
					// Errors generated updating work state indicate an underlying
					// issue accessing the database. The chainstate process will
					// be terminated as a result.
					log.Errorf("unable to confirm accepted work for block "+
						"%s: %v", header.PrevBlock.String(), err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}
				log.Infof("Mined work %s confirmed by connected block #%d",
					header.PrevBlock.String(), header.Height)

				// Signal the gui cache of the confirmed mined work.
				if cs.cfg.SignalCache != nil {
					cs.cfg.SignalCache(Confirmed)
				}

				// Process mature payments.
				err = cs.cfg.PayDividends(header.Height)
				if err != nil {
					log.Errorf("unable to process payments: %v", err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}

				// Signal the gui cache of the paid dividends.
				cs.cfg.SignalCache(DividendsPaid)
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
			confirmedWork, err := FetchAcceptedWork(cs.cfg.DB, parentID)
			if err != nil {
				// Errors generated, except for a value not found error,
				// looking up accepted work indicates an underlying issue
				// accessing the database. The chainstate process will be
				// terminated as a result.
				if !IsError(err, ErrValueNotFound) {
					log.Errorf("unable to fetch accepted work for block #%d's "+
						"parent %s : %v", header.Height, parentHash, err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}

				// If the parent of the disconnected block is not an accepted
				// work of the the pool, ignore it.
			}

			if confirmedWork != nil {
				confirmedWork.Confirmed = false
				err = confirmedWork.Update(cs.cfg.DB)
				if err != nil {
					// Errors generated updating work state indicate an underlying
					// issue accessing the database. The chainstate process will
					// be terminated as a result.
					log.Errorf("unable to unconfirm accepted work for block "+
						"%s: %v", parentHash, err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}

				log.Infof("Mined work %s unconfirmed via disconnected "+
					"block #%d", parentHash, header.Height)
			}

			// If the disconnected block is an accepted work of the pool
			// ensure it is not confirmed mined.
			blockHash := header.BlockHash().String()
			id := AcceptedWorkID(blockHash, header.Height)
			work, err := FetchAcceptedWork(cs.cfg.DB, id)
			if err != nil {
				// If the disconnected block is not an accepted
				// work of the the pool, ignore it.
				if IsError(err, ErrValueNotFound) {
					close(msg.Done)
					continue
				}

				// Errors generated, except for a value not found error,
				// looking up accepted work indicates an underlying issue
				// accessing the database. The chainstate process will be
				// terminated as a result.
				log.Errorf("unable to fetch accepted work for block #%d: %v",
					header.Height, blockHash, err)
				close(msg.Done)
				cs.cfg.Cancel()
				continue
			}

			work.Confirmed = false
			err = work.Update(cs.cfg.DB)
			if err != nil {
				// Errors generated updating work state indicate an underlying
				// issue accessing the database. The chainstate process will
				// be terminated as a result.
				log.Errorf("unable to unconfirm mined work at "+
					"height #%d: %v", err)
				close(msg.Done)
				cs.cfg.Cancel()
				continue
			}

			log.Infof("Disconnected mined work %s at height #%d",
				blockHash, header.Height)

			// Signal the gui cache of the unconfirmed (due to a reorg)
			// mined work.
			if cs.cfg.SignalCache != nil {
				cs.cfg.SignalCache(Unconfirmed)
			}

			close(msg.Done)
		}
	}
}
