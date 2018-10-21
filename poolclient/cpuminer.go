// Copyright (c) 2014-2016 The btcsuite developers
// Copyright (c) 2015-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/hex"
	"github.com/davecgh/go-spew/spew"
	"time"

	"github.com/decred/dcrd/blockchain"

	"dnldd/dcrpool/network"
)

const (
	// maxNonce is the maximum value a nonce can be in a block header.
	maxNonce = ^uint64(0) // 2^64 - 1

	// hpsUpdateSecs is the number of seconds to wait in between each
	// update to the hash rate monitor.
	hpsUpdateSecs = 5
)

// CPUMiner provides facilities for solving blocks using the CPU in a
// concurrency-safe manner. It consists of a hash rate monitor and
// worker goroutines which solve the recieved block.
type CPUMiner struct {
	c            *Client
	started      bool
	rateCh       chan float64
	updateHashes chan uint64
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
				log.Infof("Hash rate: %6.0f kilohashes/s",
					hashRate/1000)
			}

		case <-ctx.Done():
			break out
		}
	}
}

// solveBlock attempts to find some combination of an 8-bytes nonce, the pool
// assigned client pool id and current timestamp which makes the passed block
// hash to a value less than the target difficulty.
//
// This function will return early with false when conditions that trigger a
// stale block such as a new block showing up or periodically when there are
// new transactions and enough time has elapsed without finding a solution.
func (m *CPUMiner) solveBlock(ctx context.Context, decoded []byte, ticker *time.Ticker) bool {
	for {
		hashesCompleted := uint64(0)

		// Search through the entire nonce range for a solution while
		// periodically checking for early quit and stale block
		// conditions along with updates to the speed monitor.
		for nonce := uint64(0); nonce <= maxNonce; nonce++ {
			select {
			case <-m.c.ctx.Done():
				return false

			case <-ticker.C:
				m.updateHashes <- hashesCompleted
				hashesCompleted = 0

			case <-m.c.chainCh:
				// Stop current work if the chain updates or a new work
				// is received.
				return false

			default:
				// Non-blocking select to fall through
			}

			network.SetNonce(decoded, nonce)
			header, err := network.ParseBlockHeader(decoded)
			if err != nil {
				log.Error(err)
				return false
			}

			hash := header.BlockHash()
			targetDifficulty := blockchain.CompactToBig(header.Bits)
			hashesCompleted++

			// The block is solved when the new block hash is less
			// than the target difficulty.
			if blockchain.HashToBig(&hash).Cmp(targetDifficulty) <= 0 {
				m.updateHashes <- hashesCompleted
				log.Debugf("Solved block header is %v", spew.Sdump(header))
				return true
			}
		}
	}
}

// generateBlocks attempts to solve blocks them while detecting when it is
// performing stale work. When a block is solved, it is submitted.
//
// It must be run as a goroutine.
func (m *CPUMiner) generateBlocks(ctx context.Context) {
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
		workAvailable := m.c.work != nil
		m.c.workMtx.RUnlock()

		// Only proceed to mine if there is a block template available.
		if !workAvailable {
			continue
		}

		m.c.workMtx.Lock()
		decoded, err := network.DecodeHeader(m.c.work.header)
		m.c.workMtx.Unlock()
		if err != nil {
			log.Error(err)
			return
		}

		// Attempt to solve the block. The function will exit early
		// with false when conditions that trigger a stale block, so
		// a new block template can be generated. When the return is
		// true a solution was found, so submit the solved block.
		if m.solveBlock(ctx, decoded, ticker) {
			id := m.c.nextID()
			submission := network.WorkSubmissionRequest(id,
				hex.EncodeToString(decoded))

			// Record the request.
			m.c.recordRequest(*id, network.SubmitWork)

			// Submit solved block.
			m.c.connMtx.Lock()
			err := m.c.Conn.WriteJSON(submission)
			m.c.connMtx.Unlock()
			if err != nil {
				log.Debug(err)
				return
			}
		}
	}
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
		rateCh:       make(chan float64),
		updateHashes: make(chan uint64),
		c:            c,
	}
}
