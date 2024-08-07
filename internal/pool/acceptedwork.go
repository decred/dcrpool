// Copyright (c) 2019-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"time"
)

// AcceptedWork represents an accepted work submission to the network.
type AcceptedWork struct {
	UUID      string `json:"uuid"`
	BlockHash string `json:"blockhash"`
	PrevHash  string `json:"prevhash"`
	Height    uint32 `json:"height"`
	MinedBy   string `json:"minedby"`
	Miner     string `json:"miner"`
	CreatedOn int64  `json:"createdon"`

	// An accepted work becomes mined work once it is confirmed by incoming
	// work as the parent block it was built on.
	Confirmed bool `json:"confirmed"`
}

// heightToBigEndianBytes returns a 4-byte big endian representation of
// the provided block height.
func heightToBigEndianBytes(height uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, height)
	return b
}

// AcceptedWorkID generates a unique id for work accepted by the network.
func AcceptedWorkID(blockHash string, blockHeight uint32) string {
	var buf bytes.Buffer
	_, _ = buf.WriteString(hex.EncodeToString(heightToBigEndianBytes(blockHeight)))
	_, _ = buf.WriteString(blockHash)
	return buf.String()
}

// NewAcceptedWork creates an accepted work.
func NewAcceptedWork(blockHash string, prevHash string, height uint32,
	minedBy string, miner string) *AcceptedWork {
	return &AcceptedWork{
		UUID:      AcceptedWorkID(blockHash, height),
		BlockHash: blockHash,
		PrevHash:  prevHash,
		Height:    height,
		MinedBy:   minedBy,
		Miner:     miner,
		CreatedOn: time.Now().UnixNano(),
	}
}
