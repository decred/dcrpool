// Copyright (c) 2020-2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"net/http"

	"github.com/decred/dcrpool/internal/pool"
	"github.com/gorilla/csrf"
)

// accountPageData contains all of the necessary information to render the
// account template.
type accountPageData struct {
	HeaderData            headerData
	MinedWork             []*minedWork
	ArchivedPaymentsTotal string
	ArchivedPayments      []*archivedPayment
	PendingPaymentsTotal  string
	PendingPayments       []*pendingPayment
	ConnectedClients      []*client
	AccountID             string
	Address               string
	BlockExplorerURL      string
}

// account is the handler for "GET /account". Renders the account template if
// a valid address with associated account information is provided,
// otherwise renders the index template with an appropriate error message.
func (ui *GUI) account(w http.ResponseWriter, r *http.Request) {
	address := r.FormValue("address")
	if address == "" {
		ui.renderIndex(w, r, "No address provided")
		return
	}

	// Generate the account id of the provided address.
	accountID := pool.AccountID(address)

	if !ui.cfg.AccountExists(accountID) {
		ui.renderIndex(w, r, "Nothing found for address")
		return
	}

	totalPending := ui.cache.getPendingPaymentsTotal(accountID)
	totalArchived := ui.cache.getArchivedPaymentsTotal(accountID)

	// We don't need to handle errors on the following cache access because we
	// are passing hard-coded, good params.

	// Get the 10 most recently mined blocks by this account.
	_, recentWork, _ := ui.cache.getMinedWorkByAccount(0, 9, accountID)

	// Get the 10 most recent pending payments for this account.
	_, pendingPmts, _ := ui.cache.getPendingPayments(0, 9, accountID)

	// Get the 10 most recent archived payments for this account.
	_, archivedPmts, _ := ui.cache.getArchivedPayments(0, 9, accountID)

	// Get 10 of this accounts connected clients.
	_, clients, _ := ui.cache.getClientsForAccount(0, 9, accountID)

	data := &accountPageData{
		HeaderData: headerData{
			CSRF:        csrf.TemplateField(r),
			Designation: ui.cfg.Designation,
			ShowMenu:    true,
			SoloPool:    ui.cfg.SoloPool,
		},
		MinedWork:             recentWork,
		PendingPaymentsTotal:  totalPending,
		PendingPayments:       pendingPmts,
		ArchivedPaymentsTotal: totalArchived,
		ArchivedPayments:      archivedPmts,
		ConnectedClients:      clients,
		AccountID:             accountID,
		Address:               address,
		BlockExplorerURL:      ui.cfg.BlockExplorerURL,
	}

	ui.renderTemplate(w, "account", data)
}

// isPoolAccount is the handler for "HEAD /account". If the provided
// address has an account on the server a "200 OK" response is returned,
// otherwise a "400 Bad Request" or "404 Not Found" are returned.
func (ui *GUI) isPoolAccount(w http.ResponseWriter, r *http.Request) {
	address := r.FormValue("address")

	// Generate the account ID of the provided address.
	accountID := pool.AccountID(address)

	if !ui.cfg.AccountExists(accountID) {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}
