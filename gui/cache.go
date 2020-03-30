// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"math/big"
	"sync"

	"github.com/decred/dcrpool/pool"
)

// minedWork represents a block mined by the pool. It is json annotated so it
// can easily be encoded and sent over a websocket or pagination request.
type minedWork struct {
	BlockHeight uint32 `json:"blockheight"`
	BlockURL    string `json:"blockurl"`
	MinedBy     string `json:"minedby"`
	Miner       string `json:"miner"`
	Confirmed   bool   `json:"confirmed"`
	// AccountID holds the full ID (not truncated) and so should not be json encoded
	AccountID string `json:"-"`
}

// workQuota represents dividend garnered by pool accounts through work
// contributed. It is json annotated so it can easily be encoded and sent over a
// websocket or pagination request.
type workQuota struct {
	AccountID string `json:"accountid"`
	Percent   string `json:"percent"`
}

// Cache stores data which is required for the GUI. Each field has a setter and
// getter, and they are protected by a mutex so they are safe for concurrent
// access. Data returned by the getters is already formatted for display in the
// GUI, so the formatting does not need to be repeated.
type Cache struct {
	blockExplorerURL string

	minedWork    []minedWork
	minedWorkMtx sync.RWMutex

	workQuotas    []workQuota
	workQuotasMtx sync.RWMutex

	poolHash    string
	poolHashMtx sync.RWMutex
}

// InitCache initialises and returns a cache for use in the GUI.
func InitCache(work []*pool.AcceptedWork, quotas []*pool.Quota, poolHash *big.Rat, blockExplorerURL string) *Cache {
	cache := Cache{blockExplorerURL: blockExplorerURL}
	cache.updateMinedWork(work)
	cache.updateQuotas(quotas)
	cache.updateHashrate(poolHash)
	return &cache
}

func (c *Cache) updateMinedWork(work []*pool.AcceptedWork) {
	// Parse and format mined work by the pool.
	workData := make([]minedWork, 0)
	for _, work := range work {
		workData = append(workData, minedWork{
			BlockHeight: work.Height,
			BlockURL:    blockURL(c.blockExplorerURL, work.Height),
			MinedBy:     truncateAccountID(work.MinedBy),
			Miner:       work.Miner,
			AccountID:   work.MinedBy,
			Confirmed:   work.Confirmed,
		})
	}

	c.minedWorkMtx.Lock()
	c.minedWork = workData
	c.minedWorkMtx.Unlock()
}

func (c *Cache) getMinedWork() []minedWork {
	c.minedWorkMtx.RLock()
	defer c.minedWorkMtx.RUnlock()
	return c.minedWork
}

func (c *Cache) updateQuotas(quotas []*pool.Quota) {
	quotaData := make([]workQuota, 0)
	for _, quota := range quotas {
		quotaData = append(quotaData, workQuota{
			AccountID: truncateAccountID(quota.AccountID),
			Percent:   ratToPercent(quota.Percentage),
		})
	}

	c.workQuotasMtx.Lock()
	c.workQuotas = quotaData
	c.workQuotasMtx.Unlock()
}

func (c *Cache) getQuotas() []workQuota {
	c.workQuotasMtx.RLock()
	defer c.workQuotasMtx.RUnlock()
	return c.workQuotas
}

func (c *Cache) updateHashrate(poolHash *big.Rat) {
	c.poolHashMtx.Lock()
	c.poolHash = hashString(poolHash)
	c.poolHashMtx.Unlock()
}

func (c *Cache) getPoolHash() string {
	c.poolHashMtx.RLock()
	defer c.poolHashMtx.RUnlock()
	return c.poolHash
}
