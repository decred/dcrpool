// Copyright (c) 2021-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"fmt"
	"math"
	"math/big"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/v3"

	errs "github.com/decred/dcrpool/errors"
)

// Supported mining clients.
const (
	CPU               = "cpu"
	InnosiliconD9     = "innosilicond9"
	ObeliskDCR1       = "obeliskdcr1"
	NiceHashValidator = "nicehash"
)

var (
	// minerHashes is a map of all supported decred miners and their
	// corresponding hash rates.
	minerHashes = map[string]*big.Int{
		CPU:               new(big.Int).SetInt64(5e3),
		ObeliskDCR1:       new(big.Int).SetInt64(1.2e12),
		InnosiliconD9:     new(big.Int).SetInt64(2.4e12),
		NiceHashValidator: new(big.Int).SetInt64(20e10),
	}
)

// DifficultyToTarget converts the provided difficulty to a target based on the
// active network.
func DifficultyToTarget(net *chaincfg.Params, difficulty *big.Rat) *big.Rat {
	powLimit := new(big.Rat).SetInt(net.PowLimit)

	// The corresponding target is calculated as:
	//
	//    target = pow_limit / difficulty
	//
	// The result is clamped to the pow limit if it exceeds it.
	target := new(big.Rat).Quo(powLimit, difficulty)
	if target.Cmp(powLimit) > 0 {
		target = powLimit
	}
	return target
}

// calculatePoolDifficulty determines the difficulty at which the provided
// hashrate can generate a pool share by the provided target time.
func calculatePoolDifficulty(net *chaincfg.Params, hashRate *big.Int, targetTimeSecs *big.Int) *big.Rat {
	hashesPerTargetTime := new(big.Int).Mul(hashRate, targetTimeSecs)

	// The number of possible iterations is calculated as:
	//
	//    iterations := 2^(256 - floor(log2(pow_limit)))
	iterations := math.Pow(2, float64(256-net.PowLimit.BitLen()))

	// The difficulty at which the provided hashrate can mine a block is
	// calculated as:
	//
	//    difficulty = (hashes_per_sec * target_in_seconds) / iterations
	difficulty := new(big.Rat).Quo(new(big.Rat).SetInt(hashesPerTargetTime),
		new(big.Rat).SetFloat64(iterations))

	// Clamp the difficulty to 1 if needed.
	oneRat := new(big.Rat).SetInt64(1)
	if difficulty.Cmp(oneRat) < 0 {
		difficulty = oneRat
	}
	return difficulty
}

// calculatePoolTarget determines the target difficulty at which the provided
// hashrate can generate a pool share by the provided target time.
func calculatePoolTarget(net *chaincfg.Params, hashRate *big.Int, targetTimeSecs *big.Int) (*big.Rat, *big.Rat) {
	difficulty := calculatePoolDifficulty(net, hashRate, targetTimeSecs)
	target := DifficultyToTarget(net, difficulty)
	return target, difficulty
}

// DifficultyInfo represents the difficulty related info for a mining client.
type DifficultyInfo struct {
	target     *big.Rat
	difficulty *big.Rat
	powLimit   *big.Rat
}

// DifficultySet represents generated pool difficulties for supported miners.
type DifficultySet struct {
	diffs map[string]*DifficultyInfo
	mtx   sync.Mutex
}

// NewDifficultySet generates difficulty data for all supported mining clients.
func NewDifficultySet(net *chaincfg.Params, powLimit *big.Rat, maxGenTime time.Duration) *DifficultySet {
	genTime := new(big.Int).SetInt64(int64(maxGenTime.Seconds()))
	set := &DifficultySet{
		diffs: make(map[string]*DifficultyInfo),
	}
	for miner, hashrate := range minerHashes {
		target, difficulty := calculatePoolTarget(net, hashrate, genTime)
		set.diffs[miner] = &DifficultyInfo{
			target:     target,
			difficulty: difficulty,
			powLimit:   powLimit,
		}
	}
	return set
}

// fetchMinerDifficulty returns the difficulty data of the provided miner,
// if it exists.
func (d *DifficultySet) fetchMinerDifficulty(miner string) (*DifficultyInfo, error) {
	const funcName = "fetchMinerDifficulty"
	d.mtx.Lock()
	diffData, ok := d.diffs[miner]
	d.mtx.Unlock()
	if !ok {
		desc := fmt.Sprintf("%s: no difficulty data found for miner %s",
			funcName, miner)
		return nil, errs.PoolError(errs.ValueNotFound, desc)
	}
	return diffData, nil
}
