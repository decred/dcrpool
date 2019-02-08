// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"math/big"
	"testing"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg"

	"github.com/dnldd/dcrpool/dividend"
)

func TestTargetConversion(t *testing.T) {
	targetTime := new(big.Int).SetInt64(15)
	for miner, hashrate := range dividend.MinerHashes {
		target, _, err := dividend.CalculatePoolTarget(&chaincfg.MainNetParams,
			hashrate, targetTime)
		if err != nil {
			t.Error(err)
		}

		compact := blockchain.BigToCompact(target)
		leu256 := BigToLEUint256(target)
		cBig := LEUint256ToBig(leu256)
		if target.Cmp(cBig) != 0 {
			t.Errorf("invalid LEUint256 to big.Int conversion (%v) expected "+
				"%v, got %v", miner, target, cBig)
		}

		cCompact := blockchain.BigToCompact(cBig)
		if cCompact != compact {
			t.Errorf("invalid big.Int to uint32 conversion (%v) expected "+
				"%v, got %v", miner, target, cBig)
		}
	}
}
