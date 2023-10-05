// Copyright (c) 2019-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"time"
)

// ShareWeights represents the associated weights for each known DCR miner.
//
// The weights are calculated as:
//
//	Hash of Miner X / Hash of LHM
var ShareWeights = func() map[string]*big.Rat {
	// In practice there will always be at least the CPU miner, however, be safe
	// and return an empty map if the miner hashes somehow ends up empty in the
	// future.
	if len(minerHashes) == 0 {
		return make(map[string]*big.Rat, len(minerHashes))
	}

	// Find the lowest hash rate miner.
	lowestHashRate := big.NewInt(0)
	for _, hashRate := range minerHashes {
		if lowestHashRate.Sign() == 0 || hashRate.Cmp(lowestHashRate) < 0 {
			lowestHashRate.Set(hashRate)
		}
	}

	shareWeights := make(map[string]*big.Rat, len(minerHashes))
	for miner, hashRate := range minerHashes {
		// Ensure CPU is always a share weight of 1.0 as it is only valid in
		// either testing scenarios or a CPU-only mining regime.
		if miner == CPU {
			shareWeights[CPU] = new(big.Rat).SetFloat64(1.0)
			continue
		}

		shareWeights[miner] = new(big.Rat).SetFrac(hashRate, lowestHashRate)
	}
	return shareWeights
}()

// shareID generates a unique share id using the provided account, creation
// time, and random uint64.
func shareID(account string, createdOn int64, randVal uint64) string {
	var randValEncoded [8]byte
	binary.BigEndian.PutUint64(randValEncoded[:], randVal)
	var buf bytes.Buffer
	_, _ = buf.WriteString(hex.EncodeToString(nanoToBigEndianBytes(createdOn)))
	_, _ = buf.WriteString(account)
	_, _ = buf.WriteString(hex.EncodeToString(randValEncoded[:]))
	return buf.String()
}

// Share represents verifiable work performed by a pool client.
type Share struct {
	UUID      string   `json:"uuid"`
	Account   string   `json:"account"`
	Weight    *big.Rat `json:"weight"`
	CreatedOn int64    `json:"createdon"`
}

// NewShare creates a share with the provided account and weight.
func NewShare(account string, weight *big.Rat) *Share {
	now := time.Now().UnixNano()
	return &Share{
		UUID:      shareID(account, now, uuidPRNG.Uint64()),
		Account:   account,
		Weight:    weight,
		CreatedOn: now,
	}
}
