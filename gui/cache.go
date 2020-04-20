// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"fmt"
	"math/big"
	"sort"
	"sync"

	"github.com/decred/dcrpool/pool"
)

// client represents a mining client. It is json annotated so it can easily be
// encoded and sent over a websocket or pagination request.
type client struct {
	Miner    string `json:"miner"`
	IP       string `json:"ip"`
	HashRate string `json:"hashrate"`
}

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

// rewardQuota represents the percentage of reward payment garnered by a pool
// account through work contributed. It is json annotated so it can easily be
// encoded and sent over a websocket or pagination request.
type rewardQuota struct {
	AccountID string `json:"accountid"`
	Percent   string `json:"percent"`
}

// pendingPayment represents an unpaid reward payment. It is json annotated so
// it can easily be encoded and sent over a websocket or pagination request.
type pendingPayment struct {
	WorkHeight    string `json:"workheight"`
	WorkHeightURL string `json:"workheighturl"`
	Amount        string `json:"amount"`
	CreatedOn     string `json:"createdon"`
}

// archivedPayment represents a paid reward payment. It is json annotated so it
// can easily be encoded and sent over a websocket or pagination request.
type archivedPayment struct {
	WorkHeight    string `json:"workheight"`
	WorkHeightURL string `json:"workheighturl"`
	Amount        string `json:"amount"`
	CreatedOn     string `json:"createdon"`
	PaidHeight    string `json:"paidheight"`
	PaidHeightURL string `json:"paidheighturl"`
	TxURL         string `json:"txurl"`
	TxID          string `json:"txid"`
}

// Cache stores data which is required for the GUI. Each field has a setter and
// getter, and they are protected by a mutex so they are safe for concurrent
// access. Data returned by the getters is already formatted for display in the
// GUI, so the formatting does not need to be repeated.
type Cache struct {
	blockExplorerURL    string
	minedWork           []minedWork
	minedWorkMtx        sync.RWMutex
	rewardQuotas        []rewardQuota
	rewardQuotasMtx     sync.RWMutex
	poolHash            string
	poolHashMtx         sync.RWMutex
	clients             map[string][]client
	clientsMtx          sync.RWMutex
	pendingPayments     map[string][]pendingPayment
	pendingPaymentsMtx  sync.RWMutex
	archivedPayments    map[string][]archivedPayment
	archivedPaymentsMtx sync.RWMutex
}

// InitCache initialises and returns a cache for use in the GUI.
func InitCache(work []*pool.AcceptedWork, quotas []*pool.Quota,
	clients []*pool.Client, pendingPmts []*pool.Payment,
	archivedPmts []*pool.Payment, blockExplorerURL string) *Cache {

	cache := Cache{blockExplorerURL: blockExplorerURL}
	cache.updateMinedWork(work)
	cache.updateRewardQuotas(quotas)
	cache.updateClients(clients)
	cache.updatePayments(pendingPmts, archivedPmts)
	return &cache
}

// updateMinedWork refreshes the cached list of blocks mined by the pool.
func (c *Cache) updateMinedWork(work []*pool.AcceptedWork) {
	workData := make([]minedWork, 0, len(work))
	for _, w := range work {
		workData = append(workData, minedWork{
			BlockHeight: w.Height,
			BlockURL:    blockURL(c.blockExplorerURL, w.Height),
			MinedBy:     truncateAccountID(w.MinedBy),
			Miner:       w.Miner,
			AccountID:   w.MinedBy,
			Confirmed:   w.Confirmed,
		})
	}

	c.minedWorkMtx.Lock()
	c.minedWork = workData
	c.minedWorkMtx.Unlock()
}

// getMinedWork retrieves the cached list of blocks mined by the pool.
func (c *Cache) getMinedWork() []minedWork {
	c.minedWorkMtx.RLock()
	defer c.minedWorkMtx.RUnlock()
	return c.minedWork
}

// updateRewardQuotas uses a list of work quotas to refresh the cached list of
// pending reward payment quotas.
func (c *Cache) updateRewardQuotas(quotas []*pool.Quota) {

	// Sort list so the largest percentages will be shown first.
	sort.Slice(quotas, func(i, j int) bool {
		return quotas[i].Percentage.Cmp(quotas[j].Percentage) > 0
	})

	quotaData := make([]rewardQuota, 0, len(quotas))
	for _, q := range quotas {
		quotaData = append(quotaData, rewardQuota{
			AccountID: truncateAccountID(q.AccountID),
			Percent:   ratToPercent(q.Percentage),
		})
	}

	c.rewardQuotasMtx.Lock()
	c.rewardQuotas = quotaData
	c.rewardQuotasMtx.Unlock()
}

// getRewardQuotas retrieves the cached list of pending reward payment quotas.
func (c *Cache) getRewardQuotas() []rewardQuota {
	c.rewardQuotasMtx.RLock()
	defer c.rewardQuotasMtx.RUnlock()
	return c.rewardQuotas
}

// getPoolHash retrieves the total hashrate of all connected mining clients.
func (c *Cache) getPoolHash() string {
	c.poolHashMtx.RLock()
	defer c.poolHashMtx.RUnlock()
	return c.poolHash
}

// updateClients will refresh the cached list of connected clients, as well as
// recalculating the total hashrate for all connected clients.
func (c *Cache) updateClients(clients []*pool.Client) {
	clientInfo := make(map[string][]client)
	poolHashRate := new(big.Rat).SetInt64(0)
	for _, c := range clients {
		clientHashRate := c.FetchHashRate()
		accountID := c.FetchAccountID()
		clientInfo[accountID] = append(clientInfo[accountID], client{
			Miner:    c.FetchMinerType(),
			IP:       c.FetchIPAddr(),
			HashRate: hashString(clientHashRate),
		})
		poolHashRate = poolHashRate.Add(poolHashRate, clientHashRate)
	}

	c.poolHashMtx.Lock()
	c.poolHash = hashString(poolHashRate)
	c.poolHashMtx.Unlock()

	c.clientsMtx.Lock()
	c.clients = clientInfo
	c.clientsMtx.Unlock()
}

// getClients retrieves the cached list of connected clients.
func (c *Cache) getClients() map[string][]client {
	c.clientsMtx.RLock()
	defer c.clientsMtx.RUnlock()
	return c.clients
}

// updatePayments will update the cached lists of both pending and archived
// payments.
func (c *Cache) updatePayments(pendingPmts []*pool.Payment, archivedPmts []*pool.Payment) {
	pendingPayments := make(map[string][]pendingPayment)
	for _, p := range pendingPmts {
		accountID := p.Account
		pendingPayments[accountID] = append(pendingPayments[accountID], pendingPayment{
			WorkHeight:    fmt.Sprint(p.Height),
			WorkHeightURL: blockURL(c.blockExplorerURL, p.Height),
			Amount:        amount(p.Amount),
			CreatedOn:     formatUnixTime(p.CreatedOn),
		})
	}

	c.pendingPaymentsMtx.Lock()
	c.pendingPayments = pendingPayments
	c.pendingPaymentsMtx.Unlock()

	archivedPayments := make(map[string][]archivedPayment)
	for _, p := range archivedPmts {
		accountID := p.Account
		archivedPayments[accountID] = append(archivedPayments[accountID], archivedPayment{
			WorkHeight:    fmt.Sprint(p.Height),
			WorkHeightURL: blockURL(c.blockExplorerURL, p.Height),
			Amount:        amount(p.Amount),
			CreatedOn:     formatUnixTime(p.CreatedOn),
			PaidHeight:    fmt.Sprint(p.PaidOnHeight),
			PaidHeightURL: blockURL(c.blockExplorerURL, p.PaidOnHeight),
			TxURL:         txURL(c.blockExplorerURL, p.TransactionID),
			TxID:          fmt.Sprintf("%.10s...", p.TransactionID),
		})
	}

	c.archivedPaymentsMtx.Lock()
	c.archivedPayments = archivedPayments
	c.archivedPaymentsMtx.Unlock()
}

// getPendingPayments retrieves the cached list of paid payments.
func (c *Cache) getPendingPayments() map[string][]pendingPayment {
	c.pendingPaymentsMtx.RLock()
	defer c.pendingPaymentsMtx.RUnlock()
	return c.pendingPayments
}

// getArchivedPayments retrieves the cached list of unpaid payments.
func (c *Cache) getArchivedPayments() map[string][]archivedPayment {
	c.archivedPaymentsMtx.RLock()
	defer c.archivedPaymentsMtx.RUnlock()
	return c.archivedPayments
}
