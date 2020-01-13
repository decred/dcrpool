package pool

import (
	"context"
	"sync"
	"sync/atomic"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/wire"
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
	GeneratePayments func(uint32, dcrutil.Amount) error
	// GetBlock fetches the block associated with the provided block hash.
	GetBlock func(*chainhash.Hash) (*wire.MsgBlock, error)
	// Cancel represents the pool's context cancellation function.
	Cancel context.CancelFunc
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
				log.Errorf("unable to create header from bytes: %v", err)
				close(msg.Done)
				cs.cfg.Cancel()
				continue
			}
			err = cs.cfg.PayDividends(header.Height)
			if err != nil {
				log.Errorf("unable to process payments: %v", err)
				close(msg.Done)
				continue
			}
			if header.Height > MaxReorgLimit {
				pruneLimit := header.Height - MaxReorgLimit
				err := PruneJobs(cs.cfg.DB, pruneLimit)
				if err != nil {
					log.Errorf("unable to prune jobs to height %d: %v",
						pruneLimit, err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}
			}

			// If the parent of the connected block is an accepted work of the
			// pool, confirm it as mined. The parent of a connected block
			// at this point is guaranteed to have its corresponding accepted
			// work persisted if it was mined by the pool.
			parentID := AcceptedWorkID(header.PrevBlock.String(), header.Height-1)
			work, err := FetchAcceptedWork(cs.cfg.DB, parentID)
			if err != nil {
				log.Errorf("unable to fetch accepted work for block #%d's "+
					"parent %s : %v", header.Height,
					header.PrevBlock.String(), err)
				close(msg.Done)
				continue
			}
			if work == nil {
				close(msg.Done)
				continue
			}

			// Update accepted work as confirmed mined.
			work.Confirmed = true
			err = work.Update(cs.cfg.DB)
			if err != nil {
				log.Errorf("unable to confirm accepted work for block "+
					"%s: %v", header.PrevBlock.String(), err)
				close(msg.Done)
				cs.cfg.Cancel()
				continue
			}
			log.Tracef("Mined work %s confirmed by connected block #%d",
				header.PrevBlock.String(), header.Height)
			if header.Height > MaxReorgLimit {
				pruneLimit := header.Height - MaxReorgLimit
				err = PruneAcceptedWork(cs.cfg.DB, pruneLimit)
				if err != nil {
					log.Errorf("unable to prune accepted work below "+
						"height #%d: %v", pruneLimit, err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}
			}
			if !cs.cfg.SoloPool {
				block, err := cs.cfg.GetBlock(&header.PrevBlock)
				if err != nil {
					log.Errorf("unable to fetch block with hash %x: %v",
						header.PrevBlock, err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}
				coinbase := dcrutil.Amount(block.Transactions[0].TxOut[2].Value)
				err = cs.cfg.GeneratePayments(block.Header.Height, coinbase)
				if err != nil {
					log.Errorf("unable to generate shares: %v", err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}
			}
			close(msg.Done)

		case msg := <-cs.discCh:
			var header wire.BlockHeader
			err := header.FromBytes(msg.Header)
			if err != nil {
				log.Errorf("unable to create header from bytes: %v", err)
				close(msg.Done)
				cs.cfg.Cancel()
				continue
			}

			// Delete mined work if it is disconnected from the chain. At this
			// point a mined confirmed block will have its corresponding
			// accepted block record persisted.
			id := AcceptedWorkID(header.BlockHash().String(), header.Height)
			work, err := FetchAcceptedWork(cs.cfg.DB, id)
			if err != nil {
				log.Errorf("unable to fetch mined work: %v", err)
				close(msg.Done)
				continue
			}
			err = work.Delete(cs.cfg.DB)
			if err != nil {
				log.Errorf("unable to delete mined work: %v", err)
				close(msg.Done)
				cs.cfg.Cancel()
				continue
			}
			log.Tracef("Confirmed mined work %s disconnected", header.BlockHash().String())
			if !cs.cfg.SoloPool {
				// If the disconnected block is an accepted work from the pool,
				// delete all associated payments.
				payments, err := fetchPendingPaymentsAtHeight(cs.cfg.DB,
					header.Height)
				if err != nil {
					log.Errorf("failed to fetch pending payments "+
						"at height #%d: %v", header.Height, err)
					close(msg.Done)
					cs.cfg.Cancel()
					continue
				}
				for _, pmt := range payments {
					err = pmt.Delete(cs.cfg.DB)
					if err != nil {
						log.Errorf("unable to delete pending payment", err)
						close(msg.Done)
						cs.cfg.Cancel()
						break
					}
				}
			}
			close(msg.Done)
		}
	}
}
