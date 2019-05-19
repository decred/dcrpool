package gui

import (
	"fmt"
	"math/big"
	"net/http"
	"reflect"

	"github.com/decred/dcrpool/dividend"
	"github.com/decred/dcrpool/network"
)

type indexData struct {
	PoolStats    *network.PoolStats
	PoolHashRate *big.Rat
	SoloPool     bool
	AccountStats *AccountStats
	Address      string
	Admin        bool
	Error        string
}

// AccountStats is a snapshot of an accounts contribution to the pool. This
// comprises of blocks mined by the pool and payments made to the account.
type AccountStats struct {
	MinedWork []*network.AcceptedWork
	Payments  []*dividend.Payment
	Clients   []*network.ClientInfo
}

func (ui *GUI) GetIndex(w http.ResponseWriter, r *http.Request) {
	poolStats, err := ui.hub.FetchPoolStats()
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchPoolStats error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	clientInfo := ui.hub.FetchClientInfo()

	poolHashRate := new(big.Rat).SetInt64(0)
	for _, clients := range clientInfo {
		for _, client := range clients {
			poolHashRate = poolHashRate.Add(poolHashRate, client.HashRate)
		}
	}

	data := indexData{
		PoolStats:    poolStats,
		PoolHashRate: poolHashRate,
		SoloPool:     ui.cfg.SoloPool,
		Admin:        false,
	}

	address := r.FormValue("address")

	if address == "" {
		ui.renderTemplate(w, r, "index", data)
		return
	}

	data.Address = address

	if len(address) != 35 {
		data.Error = fmt.Sprintf("Address should be 35 characters")
		ui.renderTemplate(w, r, "index", data)
		return
	}

	accountID := *dividend.AccountID(address)

	work, err := ui.hub.FetchMinedWorkByAddress(accountID)
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchMinedWorkByAddress error: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	payments, err := ui.hub.FetchPaymentsForAddress(accountID)
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchPaymentsForAddress error: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	if len(work) == 0 && len(payments) == 0 {
		log.Infof("Nothing found for address: %s", address)
		data.Error = fmt.Sprintf("Nothing found for address: %s", address)
		ui.renderTemplate(w, r, "index", data)
		return
	}

	// // Reverse for display purposes. We want the most recent block/payments to be first.
	reverseSlice(work)
	reverseSlice(payments)

	data.AccountStats = &AccountStats{
		MinedWork: work,
		Payments:  payments,
		Clients:   clientInfo[accountID],
	}

	ui.renderTemplate(w, r, "index", data)
}

func reverseSlice(s interface{}) {
	size := reflect.ValueOf(s).Len()
	swap := reflect.Swapper(s)
	for i, j := 0, size-1; i < j; i, j = i+1, j-1 {
		swap(i, j)
	}
}
