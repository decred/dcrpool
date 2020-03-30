// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"net/http"

	"github.com/decred/dcrpool/pool"
	"github.com/gorilla/csrf"
)

// indexPageData contains all of the necessary information to render the index
// template.
type indexPageData struct {
	HeaderData    headerData
	PoolStatsData poolStatsData
	MinerPorts    map[string]uint32
	MinedWork     []minedWork
	PoolDomain    string
	WorkQuotas    []workQuota
	Address       string
	ModalError    string
}

// renderIndex renders the index template. It accepts an optional modalError
// which can be used to include a pop-up error message on the page. This
// function can be called from any HTTP handler which needs to display an error
// message. The error message will only be displayed if the browser has
// Javascript enabled.
func (ui *GUI) renderIndex(w http.ResponseWriter, r *http.Request, modalError string) {

	// Get the most recent confirmed mined blocks (max 10)
	work := make([]minedWork, 0)
	ui.minedWorkMtx.RLock()
	for _, v := range ui.minedWork {
		if v.Confirmed {
			work = append(work, v)
			if len(work) >= 10 {
				break
			}
		}
	}
	ui.minedWorkMtx.RUnlock()

	ui.workQuotasMtx.RLock()
	wQuotas := append(ui.workQuotas[:0:0], ui.workQuotas...)
	ui.workQuotasMtx.RUnlock()

	ui.poolHashMtx.RLock()
	poolHash := ui.poolHash
	ui.poolHashMtx.RUnlock()

	data := indexPageData{
		HeaderData: headerData{
			CSRF:        csrf.TemplateField(r),
			Designation: ui.cfg.Designation,
			ShowMenu:    true,
		},
		PoolStatsData: poolStatsData{
			LastWorkHeight:    ui.cfg.FetchLastWorkHeight(),
			LastPaymentHeight: ui.cfg.FetchLastPaymentHeight(),
			PoolHashRate:      poolHash,
			PaymentMethod:     ui.cfg.PaymentMethod,
			Network:           ui.cfg.ActiveNet.Name,
			PoolFee:           ui.cfg.PoolFee,
			SoloPool:          ui.cfg.SoloPool,
		},
		WorkQuotas: wQuotas,
		MinedWork:  work,
		PoolDomain: ui.cfg.Domain,
		MinerPorts: ui.cfg.MinerPorts,
		ModalError: modalError,
	}

	ui.renderTemplate(w, "index", data)
}

// Homepage is the handler for "GET /". It renders the index template.
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

	ui.renderIndex(w, r, "")
}
