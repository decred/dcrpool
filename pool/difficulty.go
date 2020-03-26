package pool

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/v2"
)

const (
	// Supported mining clients
	CPU           = "cpu"
	InnosiliconD9 = "innosilicond9"
	AntminerDR3   = "antminerdr3"
	AntminerDR5   = "antminerdr5"
	WhatsminerD1  = "whatsminerd1"
	ObeliskDCR1   = "obeliskdcr1"
)

var (
	// minerHashes is a map of all known DCR miners and their corresponding
	// hashrates.
	minerHashes = map[string]*big.Int{
		CPU:           new(big.Int).SetInt64(5e3),
		ObeliskDCR1:   new(big.Int).SetInt64(1.2e12),
		InnosiliconD9: new(big.Int).SetInt64(2.4e12),
		AntminerDR3:   new(big.Int).SetInt64(7.8e12),
		AntminerDR5:   new(big.Int).SetInt64(35e12),
		WhatsminerD1:  new(big.Int).SetInt64(48e12),
	}
)

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
func NewDifficultySet(net *chaincfg.Params, powLimit *big.Rat, maxGenTime time.Duration) (*DifficultySet, error) {
	genTime := new(big.Int).SetInt64(int64(maxGenTime.Seconds()))
	set := &DifficultySet{
		diffs: make(map[string]*DifficultyInfo),
	}
	for miner, hashrate := range minerHashes {
		target, difficulty, err := calculatePoolTarget(net, hashrate, genTime)
		if err != nil {
			desc := fmt.Sprintf("failed to calculate pool target for %s", miner)
			return nil, MakeError(ErrCalcPoolTarget, desc, err)
		}
		set.diffs[miner] = &DifficultyInfo{
			target:     target,
			difficulty: difficulty,
			powLimit:   powLimit,
		}
	}

	return set, nil
}

// fetchMinerDifficulty returns the difficulty data of the provided miner,
// if it exists.
func (d *DifficultySet) fetchMinerDifficulty(miner string) (*DifficultyInfo, error) {
	d.mtx.Lock()
	diffData, ok := d.diffs[miner]
	d.mtx.Unlock()
	if !ok {
		desc := fmt.Sprintf("no difficulty data found for miner %s", miner)
		return nil, MakeError(ErrValueNotFound, desc, nil)
	}
	return diffData, nil
}
