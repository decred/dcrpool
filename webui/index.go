package webui

import (
	"net/http"

	"github.com/decred/dcrpool/network"
)

type indexData struct {
	PoolStats    *network.PoolStats
	HashRate     string
	CHash        []network.ClientHashRate
	SoloPoolMode bool
	UserStats    *network.UserStats
	Address      string
	Admin        bool
}

func (ui *WebUI) GetIndex(w http.ResponseWriter, r *http.Request) {
	poolStats, err := ui.hub.FetchPoolStats()
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchPoolStats error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	reverseSlice(poolStats.MinedWork)

	poolHashRate, cHash := ui.hub.FetchHash()

	data := indexData{
		PoolStats:    poolStats,
		HashRate:     poolHashRate,
		CHash:        cHash,
		SoloPoolMode: ui.hub.SoloPoolMode(),
		Admin:        false,
	}

	address := r.FormValue("address")

	data.Address = address

	if address == "" {
		ui.renderTemplate(w, r, "index", data)
		return
	}

	resp, err := ui.hub.FetchUserStats(address)
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchUserStats error: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	reverseSlice(resp.MinedWork)
	data.UserStats = resp

	ui.renderTemplate(w, r, "index", data)
}

func reverseSlice(slice []*network.AcceptedWork) {
	for i := len(slice)/2 - 1; i >= 0; i-- {
		opp := len(slice) - 1 - i
		slice[i], slice[opp] = slice[opp], slice[i]
	}
}
