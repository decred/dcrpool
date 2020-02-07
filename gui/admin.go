// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"html/template"
	"net/http"
	"strings"

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

func (ui *GUI) GetAdmin(w http.ResponseWriter, r *http.Request) {

	session, err := ui.cookieStore.Get(r, "session")
	if err != nil {
		if !strings.Contains(err.Error(), "value is not valid") {
			log.Errorf("session error: %v", err)
			return
		}

		log.Errorf("session error: %v, new session generated", err)
	}

	if !ui.cfg.WithinLimit(session.ID, pool.APIClient) {
		http.Error(w, "Request limit exceeded", http.StatusBadRequest)
		return
	}

	if session.Values["IsAdmin"] != true {
		ui.GetIndex(w, r)
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

func (ui *GUI) PostAdmin(w http.ResponseWriter, r *http.Request) {
	session, err := ui.cookieStore.Get(r, "session")
	if err != nil {
		if !strings.Contains(err.Error(), "value is not valid") {
			log.Errorf("session error: %v", err)
			return
		}

		log.Errorf("session error: %v, new session generated", err)
	}

	if !ui.cfg.WithinLimit(session.ID, pool.APIClient) {
		http.Error(w, "Request limit exceeded", http.StatusBadRequest)
		return
	}

	pass := r.FormValue("password")

	if ui.cfg.BackupPass != pass {
		log.Warn("Unauthorized access")
		ui.GetIndex(w, r)
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

func (ui *GUI) PostLogout(w http.ResponseWriter, r *http.Request) {
	session, err := ui.cookieStore.Get(r, "session")
	if err != nil {
		if !strings.Contains(err.Error(), "value is not valid") {
			log.Errorf("session error: %v", err)
			return
		}

		log.Errorf("session error: %v, new session generated", err)
	}

	session.Values["IsAdmin"] = false
	err = session.Save(r, w)
	if err != nil {
		log.Errorf("unable to save session: %v", err)
		return
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (ui *GUI) PostBackup(w http.ResponseWriter, r *http.Request) {
	session, err := ui.cookieStore.Get(r, "session")
	if err != nil {
		if !strings.Contains(err.Error(), "value is not valid") {
			log.Errorf("session error: %v", err)
			return
		}

		log.Errorf("session error: %v, new session generated", err)
	}

	if !ui.cfg.WithinLimit(session.ID, pool.APIClient) {
		http.Error(w, "Request limit exceeded", http.StatusBadRequest)
		return
	}

	if session.Values["IsAdmin"] != true {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
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
