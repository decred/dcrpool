// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrpool/pool"
)

type indexData struct {
	MinerPorts        map[string]uint32
	LastWorkHeight    uint32
	LastPaymentHeight uint32
	MinedWork         []minedWork
	PoolHashRate      string
	PoolDomain        string
	WorkQuotas        []workQuota
	SoloPool          bool
	PaymentMethod     string
	AccountStats      *AccountStats
	Address           string
	Admin             bool
	Error             string
	BlockExplorerURL  string
	Network           string
	Designation       string
	PoolFee           float64
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
	session, err := ui.cookieStore.Get(r, "session")
	if err != nil {
		if !strings.Contains(err.Error(), "value is not valid") {
			log.Errorf("session error: %v", err)
			return
		}

		log.Errorf("session error: %v, new session generated", err)
	}

	if !ui.limiter.WithinLimit(session.ID, pool.APIClient) {
		http.Error(w, "Request limit exceeded", http.StatusBadRequest)
		return
	}

	ui.minedWorkMtx.RLock()
	mWork := append(ui.minedWork[:0:0], ui.minedWork...)
	ui.minedWorkMtx.RUnlock()

	ui.workQuotasMtx.RLock()
	wQuotas := append(ui.workQuotas[:0:0], ui.workQuotas...)
	ui.workQuotasMtx.RUnlock()

	ui.poolHashMtx.RLock()
	poolHash := ui.poolHash
	ui.poolHashMtx.RUnlock()

	data := indexData{
		WorkQuotas:        wQuotas,
		PaymentMethod:     ui.cfg.PaymentMethod,
		LastWorkHeight:    ui.hub.FetchLastWorkHeight(),
		LastPaymentHeight: ui.hub.FetchLastPaymentHeight(),
		MinedWork:         mWork,
		PoolHashRate:      poolHash,
		PoolDomain:        ui.cfg.Domain,
		SoloPool:          ui.cfg.SoloPool,
		Admin:             false,
		BlockExplorerURL:  ui.cfg.BlockExplorerURL,
		Designation:       ui.cfg.Designation,
		PoolFee:           ui.cfg.PoolFee,
		Network:           ui.cfg.ActiveNet.Name,
		MinerPorts:        ui.cfg.MinerPorts,
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
		Clients:   ui.hub.FetchAccountClientInfo(accountID),
		AccountID: accountID,
	}

	ui.renderTemplate(w, r, "index", data)
}
