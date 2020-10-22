// Copyright (c) 2014-2016 The btcsuite developers
// Copyright (c) 2015-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"time"

	"github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrpool/pool"
)

const (
	// maxNonce is the maximum value the nonce and extraNonce2 can be in a
	// block header.
	maxNonce = ^uint32(0) // 2^32 - 1

	// hpsUpdateSecs is the number of seconds to wait in between each
	// update to the hash rate monitor.
	hpsUpdateSecs = 5
)

// SubmitWorkData encapsulates fields needed to create a stratum submit message.
type SubmitWorkData struct {
	nTime       string
	nonce       string
	extraNonce2 string
}

// CPUMiner provides facilities for solving blocks using the CPU in a
// concurrency-safe manner. It consists of a hash rate monitor and
// worker goroutines which solve the received block.
type CPUMiner struct {
	miner        *Miner
	rateCh       chan float64
	updateHashes chan uint64
	workData     *SubmitWorkData
	workCh       chan *pool.Request
}

// hashRateMonitor tracks number of hashes per second the mining process is
// performing. It must be run as a goroutine.
func (m *CPUMiner) hashRateMonitor(ctx context.Context) {
	var hashRate float64
	var totalHashes uint64
	ticker := time.NewTicker(time.Second * hpsUpdateSecs)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.miner.wg.Done()
			return

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
				log.Infof("Hash rate: %6.0f kilohashes/s", hashRate/1000)
			}
		}
	}
}

// solveBlock attempts to find some combination of a 4-bytes nonce, a 4-bytes
// extraNonce1 and a 4-bytes extraNonce2 which makes the passed block
// hash to a value less than the target difficulty.
//
// This function will return early with false when conditions that trigger a
// stale block such as a new block showing up or periodically when there are
// new transactions and enough time has elapsed without finding a solution.
func (m *CPUMiner) solveBlock(ctx context.Context, headerB []byte, target *big.Rat) bool {
	ticker := time.NewTicker(333 * time.Millisecond)
	defer ticker.Stop()

	for {
		hashesCompleted := uint64(0)

		// Search through the entire nonce and extraNonce2 range for a
		// solution while periodically checking for early quit and stale
		// block conditions along with updates to the speed monitor. The break
		// condition has been intentionally omitted such that the loop will
		// continue forever until a solution is found.
		for extraNonce2 := uint32(0); ; extraNonce2++ {
			// This loop differs from the outer one in that it does not run
			// forever, thus allowing the extraNonce field to be updated
			// between each successive iteration of the regular nonce
			// space.  Note that this is achieved by placing the break
			// condition at the end of the code block, as this prevents the
			// infinite loop that would otherwise occur if we let the for
			// statement overflow the nonce value back to 0.
			for nonce := uint32(0); ; nonce++ {
				select {
				case <-ctx.Done():
					return false

				case <-ticker.C:
					select {
					case m.updateHashes <- hashesCompleted:
						hashesCompleted = 0
					default:
						// Non-blocking receive fallthrough.
					}

				case <-m.miner.chainCh:
					// Stop current work if the chain updates or a new work
					// is received.
					return false

				default:
					// Non-blocking receive fallthrough.
				}

				// Set the generated nonce.
				binary.LittleEndian.PutUint32(headerB[140:144], nonce)

				// Set the generated extraNonce2.
				binary.LittleEndian.PutUint32(headerB[148:152], extraNonce2)

				var header wire.BlockHeader
				err := header.FromBytes(headerB)
				if err != nil {
					log.Errorf("Failed to create solved block header "+
						" from bytes: %v", err)
					return false
				}

				// A valid submission is generated when the block hash is less
				// than the pool target of the client.
				hash := header.BlockHash()
				hashNum := new(big.Rat).SetInt(standalone.HashToBig(&hash))
				hashesCompleted++

				if hashNum.Cmp(target) < 0 {
					secs := uint32(header.Timestamp.Unix())
					nTimeB := make([]byte, 4)
					binary.LittleEndian.PutUint32(nTimeB, secs)
					m.workData.nTime = hex.EncodeToString(nTimeB)
					m.workData.nonce = hex.EncodeToString(headerB[140:144])
					m.workData.extraNonce2 = hex.EncodeToString(headerB[148:152])

					m.updateHashes <- hashesCompleted
					log.Infof("Solved block hash at height (%v) is (%v)",
						header.Height, header.BlockHash().String())
					return true
				}

				if nonce == maxNonce {
					break
				}
			}
		}
	}
}

// solve is the main work horse of generateblocks. It attempts to solve
// blocks while detecting when it is performing stale work. When a
// a block is solved it is sent via the work channel.
func (m *CPUMiner) solve(ctx context.Context) {
	for {
		m.miner.workMtx.RLock()
		if m.miner.work.target == nil || m.miner.work.jobID == "" ||
			m.miner.work.header == nil {
			m.miner.workMtx.RUnlock()
			time.Sleep(time.Second)
			select {
			case <-ctx.Done():
				m.miner.wg.Done()
				return

			default:
				continue
			}
		}

		headerB := make([]byte, len(m.miner.work.header))
		copy(headerB, m.miner.work.header)
		target := m.miner.work.target
		jobID := m.miner.work.jobID
		m.miner.workMtx.RUnlock()

		switch m.solveBlock(ctx, headerB, target) {
		case true:
			// Send a submit work request.
			worker := fmt.Sprintf("%s.%s", m.miner.config.Address,
				m.miner.config.User)
			id := m.miner.nextID()
			req := pool.SubmitWorkRequest(&id, worker, jobID,
				m.workData.extraNonce2, m.workData.nTime, m.workData.nonce)
			m.workCh <- req

			// Stall to prevent mining too quickly.
			time.Sleep(time.Millisecond * 500)

		case false:
			select {
			case <-ctx.Done():
				m.miner.wg.Done()
				return

			default:
				// Non-blocking receive fallthrough.
			}
		}
	}
}

// generateBlocks handles sending solved block submissions to the mining pool.
// It must be run as a goroutine.
func (m *CPUMiner) generateBlocks(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			m.miner.wg.Done()
			return

		case work := <-m.workCh:
			m.miner.recordRequest(*work.ID, pool.Submit)
			err := m.miner.encoder.Encode(work)
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}

				var nErr *net.OpError
				if errors.As(err, &nErr) {
					if nErr.Op == "write" && nErr.Net == "tcp" {
						continue
					}
				}

				log.Errorf("failed to encode work submission request: %v", err)
				m.miner.cancel()
			}
		}
	}
}

// HashesPerSecond returns the number of hashes per second the mining process
// is performing.
//
// This function is safe for concurrent access.
func (m *CPUMiner) HashesPerSecond() float64 {
	return <-m.rateCh
}

// NewCPUMiner returns a new instance of a CPU miner for the provided client.
// Use Start to begin the mining process.
func NewCPUMiner(m *Miner) *CPUMiner {
	return &CPUMiner{
		rateCh:       make(chan float64),
		updateHashes: make(chan uint64),
		workData:     new(SubmitWorkData),
		miner:        m,
		workCh:       make(chan *pool.Request),
	}
}
