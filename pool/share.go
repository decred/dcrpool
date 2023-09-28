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
// With the share weight of the lowest hash DCR miner (LHM) being 1, the
// rest were calculated as:
//
//	(Hash of Miner X * Weight of LHM)/ Hash of LHM
var ShareWeights = map[string]*big.Rat{
	CPU:         new(big.Rat).SetFloat64(1.0), // Reserved for testing.
	ObeliskDCR1: new(big.Rat).SetFloat64(1.0),
}

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
