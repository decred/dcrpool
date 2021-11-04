// Copyright (c) 2020-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"fmt"
	"math/big"
	"sort"
	"sync"

	"github.com/decred/dcrd/dcrutil/v4"
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
	WorkHeight             string `json:"workheight"`
	WorkHeightURL          string `json:"workheighturl"`
	Amount                 string `json:"amount"`
	EstimatedPaymentHeight string `json:"estimatedpaymentheight"`
}

// archivedPayment represents a paid reward payment. It is json annotated so it
// can easily be encoded and sent over a websocket or pagination request.
type archivedPayment struct {
	WorkHeight    string `json:"workheight"`
	WorkHeightURL string `json:"workheighturl"`
	Amount        string `json:"amount"`
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
	blockExplorerURL      string
	minedWork             []*minedWork
	minedWorkMtx          sync.RWMutex
	rewardQuotas          []*rewardQuota
	rewardQuotasMtx       sync.RWMutex
	poolHash              string
	poolHashMtx           sync.RWMutex
	clients               map[string][]*client
	clientsMtx            sync.RWMutex
	pendingPayments       map[string][]*pendingPayment
	pendingPaymentTotals  map[string]dcrutil.Amount
	pendingPaymentsMtx    sync.RWMutex
	archivedPayments      map[string][]*archivedPayment
	archivedPaymentTotals map[string]dcrutil.Amount
	archivedPaymentsMtx   sync.RWMutex
	lastPaymentHeight     uint32
	lastPaymentPaidOn     int64
	lastPaymentCreatedOn  int64
	lastPaymentInfoMtx    sync.RWMutex
}

// InitCache initialises and returns a cache for use in the GUI.
func InitCache(work []*pool.AcceptedWork, quotas []*pool.Quota,
	hashData map[string][]*pool.HashData, pendingPmts []*pool.Payment,
	archivedPmts []*pool.Payment, blockExplorerURL string,
	lastPmtHeight uint32, lastPmtPaidOn, lastPmtCreatedOn int64) *Cache {

	cache := Cache{blockExplorerURL: blockExplorerURL}
	cache.updateMinedWork(work)
	cache.updateRewardQuotas(quotas)
	cache.updateHashData(hashData)
	cache.updatePayments(pendingPmts, archivedPmts)
	cache.updateLastPaymentInfo(lastPmtHeight, lastPmtPaidOn, lastPmtCreatedOn)
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
	workData := make([]*minedWork, 0, len(work))
	for _, w := range work {
		workData = append(workData, &minedWork{
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
func (c *Cache) getConfirmedMinedWork(first, last int) (int, []*minedWork, error) {
	c.minedWorkMtx.RLock()
	defer c.minedWorkMtx.RUnlock()

	minedWork := make([]*minedWork, 0)
	for _, work := range c.minedWork {
		if work.Confirmed {
			minedWork = append(minedWork, work)
		}
	}

	count := len(minedWork)
	if count == 0 {
		return count, minedWork[0:0], nil
	}

	if first >= count {
		return 0, minedWork[0:0], fmt.Errorf("requested confirmed mined work "+
			"is out of range. maximum %d, requested %d", count, first)
	}

	return count, minedWork[first:min(last, count)], nil
}

// getMinedWorkByAccount retrieves the cached list of blocks mined by
// an account. Returns both confirmed and unconfirmed blocks.
func (c *Cache) getMinedWorkByAccount(first, last int, accountID string) (int, []*minedWork, error) {
	c.minedWorkMtx.RLock()
	defer c.minedWorkMtx.RUnlock()

	minedWork := make([]*minedWork, 0)
	for _, work := range c.minedWork {
		if work.AccountID == accountID {
			minedWork = append(minedWork, work)
		}
	}

	count := len(minedWork)
	if count == 0 {
		return count, minedWork[0:0], nil
	}

	if first >= count {
		return 0, minedWork[0:0], fmt.Errorf("requested mined blocks by "+
			"account is out of range. maximum %d, requested %d", count, first)
	}

	return count, minedWork[first:min(last, count)], nil
}

// updateRewardQuotas uses a list of work quotas to refresh the cached list of
// pending reward payment quotas.
func (c *Cache) updateRewardQuotas(quotas []*pool.Quota) {
	// Sort list so the largest percentages will be shown first.
	sort.Slice(quotas, func(i, j int) bool {
		return quotas[i].Percentage.Cmp(quotas[j].Percentage) > 0
	})

	quotaData := make([]*rewardQuota, 0, len(quotas))
	for _, q := range quotas {
		quotaData = append(quotaData, &rewardQuota{
			AccountID: truncateAccountID(q.AccountID),
			Percent:   ratToPercent(q.Percentage),
		})
	}

	c.rewardQuotasMtx.Lock()
	c.rewardQuotas = quotaData
	c.rewardQuotasMtx.Unlock()
}

// getRewardQuotas retrieves the cached list of pending reward payment quotas.
func (c *Cache) getRewardQuotas(first, last int) (int, []*rewardQuota, error) {
	c.rewardQuotasMtx.RLock()
	defer c.rewardQuotasMtx.RUnlock()

	count := len(c.rewardQuotas)
	if count == 0 {
		return count, c.rewardQuotas[0:0], nil
	}

	if first >= count {
		return 0, nil, fmt.Errorf("requested reward quotas is "+
			"out of range. maximum %d, requested %d", count, first)
	}

	return count, c.rewardQuotas[first:min(last, count)], nil
}

// getPoolHash retrieves the total hashrate of all connected mining clients.
func (c *Cache) getPoolHash() string {
	c.poolHashMtx.RLock()
	defer c.poolHashMtx.RUnlock()
	return c.poolHash
}

// updateHashData refreshes the cached list of  hash data from connected
// clients, as well as recalculating the total hashrate.
func (c *Cache) updateHashData(hashData map[string][]*pool.HashData) {
	clientInfo := make(map[string][]*client)
	poolHashRate := new(big.Rat).SetInt64(0)
	for _, data := range hashData {
		for _, entry := range data {
			poolHashRate = poolHashRate.Add(poolHashRate, entry.HashRate)
			clientInfo[entry.AccountID] = append(clientInfo[entry.AccountID],
				&client{
					Miner:    entry.Miner,
					IP:       entry.IP,
					HashRate: hashString(entry.HashRate),
				})
		}
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
func (c *Cache) getClientsForAccount(first, last int, accountID string) (int, []*client, error) {
	c.clientsMtx.RLock()
	defer c.clientsMtx.RUnlock()

	clients := c.clients[accountID]
	count := len(clients)
	if count == 0 {
		return count, []*client{}, nil
	}

	if first >= count {
		return 0, []*client{}, fmt.Errorf("requested clients for account "+
			"is out of range. maximum %d, requested %d", count, first)
	}

	return count, clients[first:min(last, count)], nil
}

// getClients retrieves the cached list of all clients connected to the pool.
func (c *Cache) getClients() map[string][]*client {
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

	pendingPaymentTotals := make(map[string]dcrutil.Amount)
	pendingPayments := make(map[string][]*pendingPayment)
	for _, p := range pendingPmts {
		accountID := p.Account
		pendingPayments[accountID] = append(pendingPayments[accountID],
			&pendingPayment{
				WorkHeight:             fmt.Sprint(p.Height),
				WorkHeightURL:          blockURL(c.blockExplorerURL, p.Height),
				Amount:                 amount(p.Amount),
				EstimatedPaymentHeight: fmt.Sprint(p.EstimatedMaturity + 1),
			},
		)
		if _, ok := pendingPaymentTotals[accountID]; !ok {
			pendingPaymentTotals[accountID] = dcrutil.Amount(0)
		}
		pendingPaymentTotals[accountID] += p.Amount
	}

	c.pendingPaymentsMtx.Lock()
	c.pendingPaymentTotals = pendingPaymentTotals
	c.pendingPayments = pendingPayments
	c.pendingPaymentsMtx.Unlock()

	// Sort list so the most recently earned rewards will be shown first.
	sort.Slice(archivedPmts, func(i, j int) bool {
		return archivedPmts[i].Height > archivedPmts[j].Height
	})

	archivedPaymentTotals := make(map[string]dcrutil.Amount)
	archivedPayments := make(map[string][]*archivedPayment)
	for _, p := range archivedPmts {
		accountID := p.Account
		archivedPayments[accountID] = append(archivedPayments[accountID],
			&archivedPayment{
				WorkHeight:    fmt.Sprint(p.Height),
				WorkHeightURL: blockURL(c.blockExplorerURL, p.Height),
				Amount:        amount(p.Amount),
				PaidHeight:    fmt.Sprint(p.PaidOnHeight),
				PaidHeightURL: blockURL(c.blockExplorerURL, p.PaidOnHeight),
				TxURL:         txURL(c.blockExplorerURL, p.TransactionID),
				TxID:          fmt.Sprintf("%.10s...", p.TransactionID),
			})
		if _, ok := archivedPaymentTotals[accountID]; !ok {
			archivedPaymentTotals[accountID] = dcrutil.Amount(0)
		}
		archivedPaymentTotals[accountID] += p.Amount
	}

	c.archivedPaymentsMtx.Lock()
	c.archivedPaymentTotals = archivedPaymentTotals
	c.archivedPayments = archivedPayments
	c.archivedPaymentsMtx.Unlock()
}

func (c *Cache) updateLastPaymentInfo(height uint32, paidOn, createdOn int64) {
	c.lastPaymentInfoMtx.Lock()
	c.lastPaymentHeight = height
	c.lastPaymentPaidOn = paidOn
	c.lastPaymentCreatedOn = createdOn
	c.lastPaymentInfoMtx.Unlock()
}

func (c *Cache) getLastPaymentInfo() (uint32, int64, int64) {
	c.lastPaymentInfoMtx.RLock()
	defer c.lastPaymentInfoMtx.RUnlock()
	return c.lastPaymentHeight, c.lastPaymentPaidOn, c.lastPaymentCreatedOn
}

// getArchivedPayments accesses the cached list of unpaid payments for a given
// account ID. Returns the total number of payments and the set of payments
// requested with first and last params.
func (c *Cache) getPendingPayments(first, last int, accountID string) (int, []*pendingPayment, error) {
	c.pendingPaymentsMtx.RLock()
	defer c.pendingPaymentsMtx.RUnlock()

	pendingPmts := c.pendingPayments[accountID]
	count := len(pendingPmts)
	if count == 0 {
		return count, pendingPmts[0:0], nil
	}

	if first >= count {
		return 0, pendingPmts[0:0], fmt.Errorf("requested pending payments by "+
			"account is out of range. maximum %d, requested %d",
			count, first)
	}

	return count, pendingPmts[first:min(last, count)], nil
}

// getArchivedPayments accesses the cached list of paid payments for a given
// account ID. Returns the total number of payments and the set of payments
// requested with first and last params.
func (c *Cache) getArchivedPayments(first, last int, accountID string) (int, []*archivedPayment, error) {
	c.archivedPaymentsMtx.RLock()
	defer c.archivedPaymentsMtx.RUnlock()

	archivedPmts := c.archivedPayments[accountID]
	count := len(archivedPmts)
	if count == 0 {
		return count, []*archivedPayment{}, nil
	}

	if first >= count {
		return 0, []*archivedPayment{}, fmt.Errorf("requested archived "+
			"payments by account is out of range. maximum %d, "+
			"requested %d", count, first)
	}

	return count, archivedPmts[first:min(last, count)], nil
}

func (c *Cache) getArchivedPaymentsTotal(accountID string) string {
	c.archivedPaymentsMtx.RLock()
	total := c.archivedPaymentTotals[accountID]
	c.archivedPaymentsMtx.RUnlock()
	return amount(total)
}

func (c *Cache) getPendingPaymentsTotal(accountID string) string {
	c.pendingPaymentsMtx.RLock()
	total := c.pendingPaymentTotals[accountID]
	c.pendingPaymentsMtx.RUnlock()
	return amount(total)
}
