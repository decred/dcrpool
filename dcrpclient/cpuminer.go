// Copyright (c) 2014-2016 The btcsuite developers
// Copyright (c) 2015-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/binary"
	"sync/atomic"
	"time"

	"github.com/davecgh/go-spew/spew"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
)

const (
	// maxNonce is the maximum value a nonce can be in a block header.
	maxNonce = ^uint32(0) // 2^32 - 1

	// maxExtraNonce is the maximum value an extra nonce used in a coinbase
	// transaction can be.
	maxExtraNonce = ^uint64(0) // 2^64 - 1

	// hpsUpdateSecs is the number of seconds to wait in between each
	// update to the hash rate monitor.
	hpsUpdateSecs = 15

	// maxSimnetToMine is the maximum number of blocks to mine on HEAD~1
	// for simnet so that you don't run out of memory if tickets for
	// some reason run out during simulations.
	maxSimnetToMine uint8 = 4
)

var (
	// littleEndian is a convenience variable since binary.LittleEndian is
	// quite long.
	littleEndian = binary.LittleEndian
)

// CPUMiner provides facilities for solving blocks using the CPU in a
// concurrency-safe manner. It consists of a hash rate monitor and
// worker goroutines which solve the recieved block.
type CPUMiner struct {
	c            *Client
	started      bool
	rateCh       chan float64
	updateHashes chan uint64
	// This is a map that keeps track of how many blocks have
	// been mined on each parent by the CPUMiner. It is only
	// for use in simulation networks, to diminish memory
	// exhaustion. It should not race because it's only
	// accessed in a single threaded loop below.
	minedOnParents map[chainhash.Hash]uint8
}

// hashRateMonitor tracks number of hashes per second the mining process is
// performing. It must be run as a goroutine.
func (m *CPUMiner) hashRateMonitor(ctx context.Context) {
	var hashRate float64
	var totalHashes uint64
	ticker := time.NewTicker(time.Second * hpsUpdateSecs)
	defer ticker.Stop()

out:
	for {
		select {
		case numHashes := <-m.updateHashes:
			totalHashes += numHashes

		case <-ticker.C:
			curHashRate := float64(totalHashes) / hpsUpdateSecs
			if hashRate == 0 {
				hashRate = curHashRate
			}

			hashRate = (hashRate + curHashRate) / 2
			totalHashes = 0
			if hashRate != 0 {
				log.Debugf("Hash rate: %6.0f kilohashes/s",
					hashRate/1000)
			}

		case <-ctx.Done():
			break out
		}
	}
}

// solveBlock attempts to find some combination of a nonce, extra nonce, and
// current timestamp which makes the passed block hash to a value less than the
// target difficulty. The timestamp is updated periodically and the passed
// block is modified with all tweaks during this process. This means that
// when the function returns true, the block is ready for submission.
//
// This function will return early with false when conditions that trigger a
// stale block such as a new block showing up or periodically when there are
// new transactions and enough time has elapsed without finding a solution.
func (m *CPUMiner) solveBlock(ctx context.Context, header *wire.BlockHeader, ticker *time.Ticker) bool {
	for {
		// Choose a random extra nonce offset for this block template and
		// worker. This should be done by the mining pool, it should assign a
		// mining client ID.
		enOffset, err := wire.RandomUint64()
		if err != nil {
			log.Infof("Unexpected error while generating random "+
				"extra nonce offset: %v", err)
			enOffset = 0
		}

		targetDifficulty := blockchain.CompactToBig(header.Bits)
		hashesCompleted := uint64(0)

		// Note that the entire extra nonce range is iterated and the offset is
		// added relying on the fact that overflow will wrap around 0 as
		// provided by the Go spec.
		for extraNonce := uint64(0); extraNonce < maxExtraNonce; extraNonce++ {
			// Update the extra nonce in the block template header with the
			// new value.
			littleEndian.PutUint64(header.ExtraData[:], extraNonce+enOffset)

			// Search through the entire nonce range for a solution while
			// periodically checking for early quit and stale block
			// conditions along with updates to the speed monitor.
			for i := uint32(0); i <= maxNonce; i++ {
				select {
				case <-m.c.ctx.Done():
					return false

				case <-ticker.C:
					m.updateHashes <- hashesCompleted
					hashesCompleted = 0

					// Stop current work if the chain updates or a new work
					// data with an incremented height is received.
					workHeight := atomic.LoadUint32(&m.c.workHeight)
					if workHeight > header.Height {
						return false
					}

				default:
					// Non-blocking select to fall through
				}

				// Update the nonce and hash the block header.
				header.Nonce = i
				hash := header.BlockHash()
				hashesCompleted++

				// The block is solved when the new block hash is less
				// than the target difficulty.
				if blockchain.HashToBig(&hash).Cmp(targetDifficulty) <= 0 {
					m.updateHashes <- hashesCompleted
					return true
				}
			}
		}
	}
}

// generateBlocks attempts to solve blocks them while detecting when it is
// performing stale work. When a block is solved, it is submitted.
//
// It must be run as a goroutine.
func (m *CPUMiner) generateBlocks(ctx context.Context) {
	log.Info("Starting generate blocks worker")

	// Start a ticker which is used to signal checks for stale work and
	// updates to the hash rate monitor.
	ticker := time.NewTicker(333 * time.Millisecond)
	defer ticker.Stop()

out:
	for {
		// Quit when the miner is stopped.
		select {
		case <-ctx.Done():
			break out
		default:
			// Non-blocking select to fall through
		}

		m.c.workMtx.RLock()
		workAvailable := m.c.currWork != nil
		m.c.workMtx.RUnlock()

		// Only proceed to mine if there is a block template available.
		if !workAvailable {
			continue
		}

		m.c.workMtx.Lock()
		header, err := fetchBlockHeader(m.c.currWork.header)
		m.c.workMtx.Unlock()
		if err != nil {
			log.Error(err)
			continue
		}

		// This prevents you from causing memory exhaustion issues
		// when mining aggressively in a simulation network.
		if m.minedOnParents[header.PrevBlock] >=
			maxSimnetToMine {
			log.Info("too many blocks mined on parent, stopping " +
				"until there are enough votes on these to make a new " +
				"block")
			continue
		}

		// Attempt to solve the block.  The function will exit early
		// with false when conditions that trigger a stale block, so
		// a new block template can be generated.  When the return is
		// true a solution was found, so submit the solved block.
		if m.solveBlock(ctx, header, ticker) {
			log.Infof("Block solved: %v", spew.Sdump(header))
			// block := dcrutil.NewBlock(template.Block)
			// m.submitBlock(block)
			m.minedOnParents[header.PrevBlock]++
			break out
		}
	}

	log.Info("Generate blocks worker done")
}

// Start begins the CPU mining process as well as the hash rate monitor.
// Calling this function when the CPU miner has already been started will
// have no effect.
//
// This function is safe for concurrent access.
func (m *CPUMiner) Start() {
	if !m.started {
		m.started = true
		go m.hashRateMonitor(m.c.ctx)
		go m.generateBlocks(m.c.ctx)
	}
}

// HashesPerSecond returns the number of hashes per second the mining process
// is performing. 0 is returned if the miner is not currently running.
//
// This function is safe for concurrent access.
func (m *CPUMiner) HashesPerSecond() float64 {
	// Nothing to do if the miner is not currently running.
	if !m.started {
		return 0
	}

	return <-m.rateCh
}

// newCPUMiner returns a new instance of a CPU miner for the provided server.
// Use Start to begin the mining process.
func newCPUMiner(c *Client) *CPUMiner {
	return &CPUMiner{
		rateCh:         make(chan float64),
		updateHashes:   make(chan uint64),
		minedOnParents: make(map[chainhash.Hash]uint8),
		c:              c,
	}
}
