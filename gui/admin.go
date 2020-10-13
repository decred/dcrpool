// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"net/http"

	"github.com/decred/dcrpool/pool"
	"github.com/gorilla/csrf"
	"github.com/gorilla/sessions"
)

// adminPageData contains all of the necessary information to render the admin
// template.
type adminPageData struct {
	HeaderData            headerData
	PoolStatsData         poolStatsData
	ConnectedClients      map[string][]*client
	ArchivedPaymentsTotal string
	ArchivedPayments      []*archivedPayment
	PendingPaymentsTotal  string
	PendingPayments       []*pendingPayment
}

// adminPage is the handler for "GET /admin". If the current session is
// authenticated as an admin, the admin.html template is rendered, otherwise
// returns a redirection to the homepage.
func (ui *GUI) adminPage(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*sessions.Session)

	if session.Values["IsAdmin"] != true {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	clients := ui.cache.getClients()

	totalPending := ui.cache.getPendingPaymentsTotal(pool.PoolFeesK)
	totalArchived := ui.cache.getArchivedPaymentsTotal(pool.PoolFeesK)

	// We don't need to handle errors on the following cache access because we
	// are passing hard-coded, good params.

	// Get the 10 most recent pending payments.
	_, pendingPmts, _ := ui.cache.getPendingPayments(0, 9, pool.PoolFeesK)

	// Get the 10 most recent archived payments.
	_, archivedPmts, _ := ui.cache.getArchivedPayments(0, 9, pool.PoolFeesK)

	// TODO: This func also returns last payment paid-on time and created-on
	// time, but the GUI doesn't use them yet.
	lastPaymentHeight, _, _ := ui.cache.getLastPaymentInfo()

	pageData := adminPageData{
		HeaderData: headerData{
			CSRF:        csrf.TemplateField(r),
			Designation: ui.cfg.Designation,
			ShowMenu:    false,
		},
		PoolStatsData: poolStatsData{
			LastWorkHeight:    ui.cfg.FetchLastWorkHeight(),
			LastPaymentHeight: lastPaymentHeight,
			PoolHashRate:      ui.cache.getPoolHash(),
			PaymentMethod:     ui.cfg.PaymentMethod,
			Network:           ui.cfg.ActiveNet.Name,
			PoolFee:           ui.cfg.PoolFee,
			SoloPool:          ui.cfg.SoloPool,
		},
		ConnectedClients:      clients,
		PendingPaymentsTotal:  totalPending,
		PendingPayments:       pendingPmts,
		ArchivedPaymentsTotal: totalArchived,
		ArchivedPayments:      archivedPmts,
	}

	ui.renderTemplate(w, "admin", pageData)
}

// adminLogin is the handler for "POST /admin". If proper admin credentials are
// supplied, the session is authenticated and a "200 OK" response is returned,
// otherwise a "401 Unauthorized" response is returned.
func (ui *GUI) adminLogin(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*sessions.Session)

	pass := r.FormValue("password")

	if ui.cfg.AdminPass != pass {
		log.Warn("Unauthorized access")
		http.Error(w, "Incorrect password", http.StatusUnauthorized)
		return
	}

	session.Values["IsAdmin"] = true
	err := session.Save(r, w)
	if err != nil {
		log.Errorf("unable to save session: %v", err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// adminLogout is the handler for "POST /logout". The admin authentication is
// removed from the current session and the request is redirected to the
// homepage handler.
func (ui *GUI) adminLogout(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*sessions.Session)

	session.Values["IsAdmin"] = false
	err := session.Save(r, w)
	if err != nil {
		log.Errorf("unable to save session: %v", err)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// downloadDatabaseBackup is the handler for "POST /backup". If the current
// session is authenticated as an admin, a binary representation of the whole
// database is generated and returned to the client.
func (ui *GUI) downloadDatabaseBackup(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*sessions.Session)

	if session.Values["IsAdmin"] != true {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	err := ui.cfg.BackupDB(w)
	if err != nil {
		log.Errorf("Error backing up database: %v", err)
		// Don't write any http body here because Content-Length has already
		// been set in BackupDB. Status is enough to indicate an error.
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
