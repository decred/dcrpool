// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"net/http"

	"github.com/decred/dcrpool/pool"
	"github.com/gorilla/csrf"
)

// accountPageData contains all of the necessary information to render the
// account template.
type accountPageData struct {
	HeaderData       headerData
	MinedWork        *[]minedWork
	ArchivedPayments *[]archivedPayment
	PendingPayments  *[]pendingPayment
	PaymentRequested bool
	ConnectedClients *[]client
	AccountID        string
	Address          string
	BlockExplorerURL string
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
	accountID, err := pool.AccountID(address, ui.cfg.ActiveNet)
	if err != nil {
		ui.renderIndex(w, r, "Unable to generate account ID for address")
		return
	}

	if !ui.cfg.AccountExists(accountID) {
		ui.renderIndex(w, r, "Nothing found for address")
		return
	}

	// Get the 10 most recently mined blocks by this account.
	_, recentWork := ui.cache.getMinedWorkByAccount(0, 9, accountID)

	// Get the 10 most recent pending payments for this account.
	_, pendingPmts := ui.cache.getPendingPayments(0, 9, accountID)

	// Get the 10 most recent archived payments for this account.
	_, archivedPmts := ui.cache.getArchivedPayments(0, 9, accountID)

	// Get 10 of this accounts connected clients.
	_, clients := ui.cache.getClientsForAccount(0, 9, accountID)

	data := &accountPageData{
		HeaderData: headerData{
			CSRF:        csrf.TemplateField(r),
			Designation: ui.cfg.Designation,
			ShowMenu:    true,
		},
		MinedWork:        recentWork,
		PendingPayments:  pendingPmts,
		ArchivedPayments: archivedPmts,
		// This is a workaround to hide the process payments feature, it will
		// be removed completely in a separate PR>
		PaymentRequested: true,
		ConnectedClients: clients,
		AccountID:        accountID,
		Address:          address,
		BlockExplorerURL: ui.cfg.BlockExplorerURL,
	}

	ui.renderTemplate(w, "account", data)
}

// isPoolAccount is the handler for "HEAD /account". If the provided
// address has an account on the server a "200 OK" response is returned,
// otherwise a "400 Bad Request" or "404 Not Found" are returned.
func (ui *GUI) isPoolAccount(w http.ResponseWriter, r *http.Request) {

	address := r.FormValue("address")

	accountID, err := pool.AccountID(address, ui.cfg.ActiveNet)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !ui.cfg.AccountExists(accountID) {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// requestPayment is the handler for "POST /requestpayment". It will request a
// payment for the provided address and return a "200 OK" response if
// successful, otherwise return a "400 Bad Request".
func (ui *GUI) requestPayment(w http.ResponseWriter, r *http.Request) {
	address := r.FormValue("address")

	err := ui.cfg.AddPaymentRequest(address)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}
