// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"net/http"

	"github.com/gorilla/csrf"
)

// indexPageData contains all of the necessary information to render the index
// template.
type indexPageData struct {
	HeaderData    headerData
	PoolStatsData poolStatsData
	MinerPorts    map[string]uint32
	MinedWork     []minedWork
	RewardQuotas  []rewardQuota
	Address       string
	ModalError    string
}

// renderIndex renders the index template. It accepts an optional modalError
// which can be used to include a pop-up error message on the page. This
// function can be called from any HTTP handler which needs to display an error
// message. The error message will only be displayed if the browser has
// Javascript enabled.
func (ui *GUI) renderIndex(w http.ResponseWriter, r *http.Request, modalError string) {

	// Get the 10 most recent confirmed mined blocks.
	_, confirmedWork, _ := ui.cache.getConfirmedMinedWork(0, 9)

	// Get the first 10 next reward payment percentages.
	_, rewardQuotas, _ := ui.cache.getRewardQuotas(0, 9)

	data := indexPageData{
		HeaderData: headerData{
			CSRF:        csrf.TemplateField(r),
			Designation: ui.cfg.Designation,
			ShowMenu:    true,
		},
		PoolStatsData: poolStatsData{
			LastWorkHeight:    ui.cfg.FetchLastWorkHeight(),
			LastPaymentHeight: ui.cfg.FetchLastPaymentHeight(),
			PoolHashRate:      ui.cache.getPoolHash(),
			PaymentMethod:     ui.cfg.PaymentMethod,
			Network:           ui.cfg.ActiveNet.Name,
			PoolFee:           ui.cfg.PoolFee,
			SoloPool:          ui.cfg.SoloPool,
		},
		RewardQuotas: rewardQuotas,
		MinedWork:    confirmedWork,
		MinerPorts:   ui.cfg.MinerPorts,
		ModalError:   modalError,
	}

	ui.renderTemplate(w, "index", data)
}

// homepage is the handler for "GET /". It renders the index template.
func (ui *GUI) homepage(w http.ResponseWriter, r *http.Request) {
	ui.renderIndex(w, r, "")
}
