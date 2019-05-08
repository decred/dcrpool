package webui

import (
	"fmt"
	"math/big"
	"net/http"

	"github.com/decred/dcrpool/dividend"
	"github.com/decred/dcrpool/network"
)

type indexData struct {
	PoolStats    *network.PoolStats
	PoolHashRate *big.Rat
	SoloPoolMode bool
	UserStats    *UserStats
	Address      string
	Admin        bool
	Error        string
}

type UserStats struct {
	MinedWork []*network.AcceptedWork
	Payments  []*dividend.Payment
	Clients   []*network.ClientInfo
}

func (ui *WebUI) GetIndex(w http.ResponseWriter, r *http.Request) {
	poolStats, err := ui.hub.FetchPoolStats()
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchPoolStats error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Reverse for display purposes. We want the most recent block to be first.
	reverseSlice(poolStats.MinedWork)

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
		SoloPoolMode: ui.hub.SoloPoolMode(),
		Admin:        false,
	}

	address := r.FormValue("address")

	data.Address = address

	if address == "" {
		ui.renderTemplate(w, r, "index", data)
		return
	}

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

	// Reverse for display purposes. We want the most recent block to be first.
	reverseSlice(work)
	data.UserStats = &UserStats{
		MinedWork: work,
		Payments:  payments,
		Clients:   clientInfo[accountID],
	}

	ui.renderTemplate(w, r, "index", data)
}

func reverseSlice(slice []*network.AcceptedWork) {
	for i := len(slice)/2 - 1; i >= 0; i-- {
		opp := len(slice) - 1 - i
		slice[i], slice[opp] = slice[opp], slice[i]
	}
}
