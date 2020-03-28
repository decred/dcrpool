// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/decred/dcrpool/pool"
	"github.com/gorilla/csrf"
)

type indexData struct {
	MinerPorts       map[string]uint32
	MinedWork        []minedWork
	PoolDomain       string
	PoolStats        poolStats
	WorkQuotas       []workQuota
	AccountStats     *AccountStats
	Address          string
	Admin            bool
	Error            string
	BlockExplorerURL string
	Designation      string
	CSRF             template.HTML
}

// AccountStats is a snapshot of an accounts contribution to the pool. This is
// comprised of blocks mined by the pool and payments made to the account.
type AccountStats struct {
	MinedWork []*pool.AcceptedWork
	Payments  []*pool.Payment
	Clients   []*pool.ClientInfo
	AccountID string
}

// Homepage is the handler for "GET /". If a valid address parameter is
// provided, the account.html template is rendered and populated with the
// relevant account information, otherwise the index.html template is rendered.
func (ui *GUI) Homepage(w http.ResponseWriter, r *http.Request) {
	session, err := getSession(r, ui.cookieStore)
	if err != nil {
		log.Errorf("getSession error: %v", err)
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}

	if !ui.cfg.WithinLimit(session.ID, pool.APIClient) {
		http.Error(w, "Request limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Get the most recently mined blocks (max 10)
	ui.minedWorkMtx.RLock()
	lastBlock := 10
	count := len(ui.minedWork)
	if count < 10 {
		lastBlock = count
	}
	mWork := ui.minedWork[0:lastBlock]
	ui.minedWorkMtx.RUnlock()

	ui.workQuotasMtx.RLock()
	wQuotas := append(ui.workQuotas[:0:0], ui.workQuotas...)
	ui.workQuotasMtx.RUnlock()

	ui.poolHashMtx.RLock()
	poolHash := ui.poolHash
	ui.poolHashMtx.RUnlock()

	poolStats := poolStats{
		LastWorkHeight:    ui.cfg.FetchLastWorkHeight(),
		LastPaymentHeight: ui.cfg.FetchLastPaymentHeight(),
		PoolHashRate:      poolHash,
		PaymentMethod:     ui.cfg.PaymentMethod,
		Network:           ui.cfg.ActiveNet.Name,
		PoolFee:           ui.cfg.PoolFee,
		SoloPool:          ui.cfg.SoloPool,
	}

	data := indexData{
		WorkQuotas:       wQuotas,
		MinedWork:        mWork,
		PoolDomain:       ui.cfg.Domain,
		PoolStats:        poolStats,
		Admin:            false,
		BlockExplorerURL: ui.cfg.BlockExplorerURL,
		Designation:      ui.cfg.Designation,
		MinerPorts:       ui.cfg.MinerPorts,
		CSRF:             csrf.TemplateField(r),
	}

	address := r.FormValue("address")
	if address == "" {
		ui.renderTemplate(w, "index", data)
		return
	}

	// Add address to template data so the input field can be repopulated
	// with the users input.
	data.Address = address

	// Generate the account id of the provided address.
	accountID, err := pool.AccountID(address, ui.cfg.ActiveNet)
	if err != nil {
		data.Error = fmt.Sprintf("Unable to generate account ID for address %s", address)
		ui.renderTemplate(w, "index", data)
		return
	}

	if !ui.cfg.AccountExists(accountID) {
		data.Error = fmt.Sprintf("Nothing found for address %s", address)
		ui.renderTemplate(w, "index", data)
		return
	}

	work, err := ui.cfg.FetchMinedWorkByAccount(accountID)
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchMinedWorkByAddress error: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	payments, err := ui.cfg.FetchPaymentsForAccount(accountID)
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchPaymentsForAddress error: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	data.AccountStats = &AccountStats{
		MinedWork: work,
		Payments:  payments,
		Clients:   ui.cfg.FetchAccountClientInfo(accountID),
		AccountID: accountID,
	}

	ui.renderTemplate(w, "account", data)
}

// IsPoolAccount is the handler for "GET /account". If the provided address
// has an account on the server a "200 OK" response is returned, otherwise a
// "400 Bad Request" is returned.
func (ui *GUI) IsPoolAccount(w http.ResponseWriter, r *http.Request) {
	session, err := getSession(r, ui.cookieStore)
	if err != nil {
		log.Errorf("getSession error: %v", err)
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}

	if !ui.cfg.WithinLimit(session.ID, pool.APIClient) {
		http.Error(w, "Request limit exceeded", http.StatusTooManyRequests)
		return
	}

	address := r.FormValue("address")

	accountID, err := pool.AccountID(address, ui.cfg.ActiveNet)
	if err != nil {
		http.Error(w, "Invalid address", http.StatusBadRequest)
		return
	}

	if !ui.cfg.AccountExists(accountID) {
		http.Error(w, "Nothing found for address", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}
