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

// min returns the smaller of the two provided integers.
func min(x, y int) int {
	if x < y {
		return x
	}
	return y
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

// getConfirmedMinedWork retrieves the cached list of confirmed blocks mined by
// the pool.
func (c *Cache) getConfirmedMinedWork(first, last int) (int, []minedWork, error) {
	c.minedWorkMtx.RLock()
	defer c.minedWorkMtx.RUnlock()

	allWork := make([]minedWork, 0)
	for _, v := range c.minedWork {
		if v.Confirmed {
			allWork = append(allWork, v)
		}
	}

	count := len(allWork)

	if first >= count {
		return 0, allWork[0:0], fmt.Errorf("blocks request is out of range. maximum %v, requested %v", count, first)
	}

	requestedWork := allWork[first:min(last, count)]

	return count, requestedWork, nil
}

// getMinedWorkByAccount retrieves the cached list of blocks mined by
// an account. Returns both confirmed and unconfirmed blocks.
func (c *Cache) getMinedWorkByAccount(first, last int, accountID string) (int, []minedWork, error) {
	c.minedWorkMtx.RLock()
	defer c.minedWorkMtx.RUnlock()

	allWork := make([]minedWork, 0)
	for _, v := range c.minedWork {
		if v.AccountID == accountID {
			allWork = append(allWork, v)
		}
	}

	count := len(allWork)

	if first >= count {
		return 0, allWork[0:0], fmt.Errorf("blocks by account request is out of range. maximum %v, requested %v", count, first)
	}

	requestedWork := allWork[first:min(last, count)]

	return count, requestedWork, nil
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
func (c *Cache) getRewardQuotas(first, last int) (int, []rewardQuota, error) {
	c.rewardQuotasMtx.RLock()
	defer c.rewardQuotasMtx.RUnlock()

	count := len(c.rewardQuotas)

	if first >= count {
		return 0, c.rewardQuotas[0:0], fmt.Errorf("reward quotas request is out of range. maximum %v, requested %v", count, first)
	}

	requestedQuotas := c.rewardQuotas[first:min(last, count)]

	return count, requestedQuotas, nil
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

// getClientsForAccount retrieves the cached list of connected clients for a
// given account ID.
func (c *Cache) getClientsForAccount(first, last int, accountID string) (int, []client, error) {
	c.clientsMtx.RLock()
	defer c.clientsMtx.RUnlock()

	accountClients := c.clients[accountID]

	count := len(accountClients)

	if first >= count {
		return 0, accountClients[0:0], fmt.Errorf("clients by account request is out of range. maximum %v, requested %v", count, first)
	}

	requestedClients := accountClients[first:min(last, count)]

	return count, requestedClients, nil
}

// getClients retrieves the cached list of all clients connected to the pool.
func (c *Cache) getClients() map[string][]client {
	c.clientsMtx.RLock()
	defer c.clientsMtx.RUnlock()
	return c.clients
}

// updatePayments will update the cached lists of both pending and archived
// payments.
func (c *Cache) updatePayments(pendingPmts []*pool.Payment, archivedPmts []*pool.Payment) {

	// Sort list so the most recently earned rewards will be shown first.
	sort.Slice(pendingPmts, func(i, j int) bool {
		return pendingPmts[i].Height > pendingPmts[j].Height
	})

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

	// Sort list so the most recently earned rewards will be shown first.
	sort.Slice(archivedPmts, func(i, j int) bool {
		return archivedPmts[i].Height > archivedPmts[j].Height
	})

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

// getPendingPayments retrieves the cached list of unpaid payments for a given
// account ID.
func (c *Cache) getPendingPayments(first, last int, accountID string) (int, []pendingPayment, error) {
	c.pendingPaymentsMtx.RLock()
	defer c.pendingPaymentsMtx.RUnlock()

	accountPayments := c.pendingPayments[accountID]

	count := len(accountPayments)

	if first >= count {
		return 0, accountPayments[0:0], fmt.Errorf("pending payments by account request is out of range. maximum %v, requested %v", count, first)
	}

	requestedPayments := accountPayments[first:min(last, count)]

	return count, requestedPayments, nil
}

// getArchivedPayments retrieves the cached list of paid payments for a given
// account ID.
func (c *Cache) getArchivedPayments(first, last int, accountID string) (int, []archivedPayment, error) {
	c.archivedPaymentsMtx.RLock()
	defer c.archivedPaymentsMtx.RUnlock()

	accountPayments := c.archivedPayments[accountID]

	count := len(accountPayments)

	if first >= count {
		return 0, accountPayments[0:0], fmt.Errorf("archived payments by account request is out of range. maximum %v, requested %v", count, first)
	}

	requestedPayments := accountPayments[first:min(last, count)]

	return count, requestedPayments, nil
}
