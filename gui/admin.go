// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"html/template"
	"net/http"

	"github.com/gorilla/csrf"

	"github.com/decred/dcrpool/pool"
)

type adminPageData struct {
	Connections map[string][]*pool.ClientInfo
	CSRF        template.HTML
	Designation string
	PoolStats   poolStats
	Admin       bool
}

// AdminPage is the handler for "GET /admin". If the current session is
// authenticated as an admin, the admin.html template is rendered, otherwise
// returns a redirection to the homepage.
func (ui *GUI) AdminPage(w http.ResponseWriter, r *http.Request) {
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

	if session.Values["IsAdmin"] != true {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

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

	pageData := adminPageData{
		CSRF:        csrf.TemplateField(r),
		Designation: ui.cfg.Designation,
		Connections: ui.cfg.FetchClientInfo(),
		PoolStats:   poolStats,
		Admin:       true,
	}

	ui.renderTemplate(w, r, "admin", pageData)
}

// AdminLogin is the handler for "POST /admin". If proper admin credentials are
// supplied, the session is authenticated and the admin.html template is
// rendered, otherwise returns a redirection to the homepage.
func (ui *GUI) AdminLogin(w http.ResponseWriter, r *http.Request) {
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

	pass := r.FormValue("password")

	if ui.cfg.AdminPass != pass {
		log.Warn("Unauthorized access")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	session.Values["IsAdmin"] = true
	err = session.Save(r, w)
	if err != nil {
		log.Errorf("unable to save session: %v", err)
		return
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// AdminLogout is the handler for "POST /logout". The admin authentication is
// removed from the current session and the request is redirected to the
// homepage handler.
func (ui *GUI) AdminLogout(w http.ResponseWriter, r *http.Request) {
	session, err := getSession(r, ui.cookieStore)
	if err != nil {
		log.Errorf("getSession error: %v", err)
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}

	session.Values["IsAdmin"] = false
	err = session.Save(r, w)
	if err != nil {
		log.Errorf("unable to save session: %v", err)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// DownloadDatabaseBackup is the handler for "POST /backup". If the current
// session is authenticated as an admin, a binary representation of the whole
// database is generated and returned to the client.
func (ui *GUI) DownloadDatabaseBackup(w http.ResponseWriter, r *http.Request) {
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

	if session.Values["IsAdmin"] != true {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	err = ui.cfg.BackupDB(w)
	if err != nil {
		log.Errorf("Error backing up database: %v", err)
		http.Error(w, "Error backing up database: "+err.Error(),
			http.StatusInternalServerError)
		return
	}
}
