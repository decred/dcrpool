// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"fmt"
	"net/http"

	"github.com/decred/dcrpool/pool"
	"github.com/gorilla/csrf"
)

// accountPageData contains all of the necessary information to render the
// account template.
type accountPageData struct {
	HeaderData       headerData
	MinedWork        []minedWork
	Payments         []*pool.Payment
	ConnectedClients []client
	AccountID        string
	Address          string
	BlockExplorerURL string
}

// Account is the handler for "GET /account". Renders the account template if
// a valid address with associated account information is provided,
// otherwise renders the index template with an appropriate error message.
func (ui *GUI) Account(w http.ResponseWriter, r *http.Request) {

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

	// Get the most recently mined blocks by this account (max 10).
	allWork := ui.cache.getMinedWork()
	recentWork := make([]minedWork, 0)
	for _, v := range allWork {
		if v.AccountID == accountID {
			recentWork = append(recentWork, v)
			if len(recentWork) >= 10 {
				break
			}
		}
	}

	payments, err := ui.cfg.FetchPaymentsForAccount(accountID)
	if err != nil {
		ui.renderIndex(w, r, fmt.Sprintf("FetchPaymentsForAddress error: %v",
			err.Error()))
		return
	}

	// Get this accounts connected clients (max 10).
	clients := ui.cache.getClients()[accountID]
	if len(clients) > 10 {
		clients = clients[0:10]
	}

	data := &accountPageData{
		HeaderData: headerData{
			CSRF:        csrf.TemplateField(r),
			Designation: ui.cfg.Designation,
			ShowMenu:    true,
		},
		MinedWork:        recentWork,
		Payments:         payments,
		ConnectedClients: clients,
		AccountID:        accountID,
		Address:          address,
		BlockExplorerURL: ui.cfg.BlockExplorerURL,
	}

	ui.renderTemplate(w, "account", data)
}

// IsPoolAccount is the handler for "HEAD /account". If the provided
// address has an account on the server a "200 OK" response is returned,
// otherwise a "400 Bad Request" or "404 Not Found" are returned.
func (ui *GUI) IsPoolAccount(w http.ResponseWriter, r *http.Request) {

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
