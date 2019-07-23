// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"fmt"
	"net/http"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrpool/pool"
)

type indexData struct {
	MinerPorts       map[string]uint32
	PoolStats        *pool.Stats
	PoolDomain       string
	WorkQuotas       []pool.Quota
	SoloPool         bool
	PaymentMethod    string
	AccountStats     *AccountStats
	Address          string
	Admin            bool
	Error            string
	BlockExplorerURL string
	Network          string
	Designation      string
	PoolFee          float64
}

// AccountStats is a snapshot of an accounts contribution to the pool. This
// comprises of blocks mined by the pool and payments made to the account.
type AccountStats struct {
	MinedWork []*pool.AcceptedWork
	Payments  []*pool.Payment
	Clients   []*pool.ClientInfo
	AccountID string
}

func (ui *GUI) GetIndex(w http.ResponseWriter, r *http.Request) {
	poolStats, err := ui.hub.FetchStats()
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchPoolStats error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	workQuotas, err := ui.hub.FetchWorkQuotas()
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchWorkQuotas error: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	data := indexData{
		WorkQuotas:       workQuotas,
		PaymentMethod:    ui.cfg.PaymentMethod,
		PoolStats:        poolStats,
		PoolDomain:       ui.cfg.Domain,
		SoloPool:         ui.cfg.SoloPool,
		Admin:            false,
		BlockExplorerURL: ui.cfg.BlockExplorerURL,
		Designation:      ui.cfg.Designation,
		PoolFee:          ui.cfg.PoolFee,
		Network:          ui.cfg.ActiveNet.Name,
		MinerPorts:       ui.cfg.MinerPorts,
	}

	address := r.FormValue("address")

	if address == "" {
		ui.renderTemplate(w, r, "index", data)
		return
	}

	// Add address to template data so the input field can be repopulated
	// with the users input.
	data.Address = address

	// Ensure the provided address is valid.
	addr, err := dcrutil.DecodeAddress(address)
	if err != nil {
		data.Error = fmt.Sprintf("Failed to decode address")
		ui.renderTemplate(w, r, "index", data)
		return
	}

	// Ensure address is on the active network.
	if !addr.IsForNet(ui.cfg.ActiveNet) {
		data.Error = fmt.Sprintf("Invalid %s addresss", ui.cfg.ActiveNet.Name)
		ui.renderTemplate(w, r, "index", data)
		return
	}

	accountID, err := pool.AccountID(address)
	if err != nil {
		data.Error = fmt.Sprintf("Unable to generate account ID for address %s", address)
		ui.renderTemplate(w, r, "index", data)
		return
	}

	if !ui.hub.AccountExists(accountID) {
		data.Error = fmt.Sprintf("Nothing found for address %s", address)
		ui.renderTemplate(w, r, "index", data)
		return
	}

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
		data.Error = fmt.Sprintf("No confirmed work for %s", address)
		ui.renderTemplate(w, r, "index", data)
		return
	}

	data.AccountStats = &AccountStats{
		MinedWork: work,
		Payments:  payments,
		Clients:   poolStats.Clients[accountID],
		AccountID: accountID,
	}

	ui.renderTemplate(w, r, "index", data)
}
