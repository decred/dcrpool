// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"fmt"
	"net/http"

	"github.com/decred/dcrpool/pool"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
)

// accountPageData contains all of the necessary information to render the
// account template.
type accountPageData struct {
	HeaderData       headerData
	MinedWork        []*pool.AcceptedWork
	Payments         []*pool.Payment
	Clients          []*pool.ClientInfo
	AccountID        string
	Address          string
	BlockExplorerURL string
}

// Account is the handler for "GET /account/{address}". Renders the account
// template if the provided address is valid and has associated account
// information, otherwise renders the index template with an appopriate error
// message.
func (ui *GUI) Account(w http.ResponseWriter, r *http.Request) {
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

	address := mux.Vars(r)["address"]
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

	work, err := ui.cfg.FetchMinedWorkByAccount(accountID)
	if err != nil {
		ui.renderIndex(w, r, fmt.Sprintf("FetchMinedWorkByAddress error: %v",
			err.Error()))
		return

	}

	payments, err := ui.cfg.FetchPaymentsForAccount(accountID)
	if err != nil {
		ui.renderIndex(w, r, fmt.Sprintf("FetchPaymentsForAddress error: %v",
			err.Error()))
		return
	}

	data := &accountPageData{
		HeaderData: headerData{
			CSRF:        csrf.TemplateField(r),
			Designation: ui.cfg.Designation,
			ShowMenu:    true,
		},
		MinedWork:        work,
		Payments:         payments,
		Clients:          ui.cfg.FetchAccountClientInfo(accountID),
		AccountID:        accountID,
		Address:          address,
		BlockExplorerURL: ui.cfg.BlockExplorerURL,
	}

	ui.renderTemplate(w, "account", data)
}

// IsPoolAccount is the handler for "GET /account_exist". If the provided
// address has an account on the server a "200 OK" response is returned,
// otherwise a "400 Bad Request" is returned.
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
