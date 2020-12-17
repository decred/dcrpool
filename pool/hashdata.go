// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"math/big"
	"time"
)

// HashData represents client identification and hashrate information
// for a mining client.
type HashData struct {
	UUID      string   `json:"uuid"`
	AccountID string   `json:"accountid"`
	Miner     string   `json:"miner"`
	IP        string   `json:"ip"`
	HashRate  *big.Rat `json:"hashrate"`
	UpdatedOn int64    `json:"updatedon"`
}

// hashDataID generates a unique hash data id.
func hashDataID(accountID string, extraNonce1 string) string {
	var buf bytes.Buffer
	_, _ = buf.WriteString(extraNonce1)
	_, _ = buf.WriteString(accountID)
	return buf.String()
}

// newHashData creates a new hash data.
func newHashData(miner string, accountID string, ip string, extraNonce1 string, hashRate *big.Rat) *HashData {
	nowNano := time.Now().UnixNano()
	return &HashData{
		UUID:      hashDataID(accountID, extraNonce1),
		AccountID: accountID,
		HashRate:  hashRate,
		IP:        ip,
		Miner:     miner,
		UpdatedOn: nowNano,
	}
}
