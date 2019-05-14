package gui

import (
	"net/http"

	"github.com/decred/dcrpool/network"
)

type indexData struct {
	PoolStats    *network.PoolStats
	HashRate     string
	HashSet      []network.ClientHash
	MinedWork    *network.MinedWork
	SoloPool     bool
	AccountStats *network.AccountStats
	Address      string
	Admin        bool
}

func (ui *GUI) GetIndex(w http.ResponseWriter, r *http.Request) {
	poolStats, err := ui.hub.FetchPoolStats()
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchPoolStats error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	poolHashRate, hashSet := ui.hub.FetchHash()

	data := indexData{
		PoolStats: poolStats,
		HashRate:  poolHashRate,
		HashSet:   hashSet,
		SoloPool:  ui.cfg.SoloPool,
		Admin:     false,
	}

	address := r.FormValue("address")

	if address == "" {
		ui.renderTemplate(w, r, "index", data)
		return
	}

	data.Address = address

	minedWork, err := ui.hub.FetchMinedWork(true)
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchMinedWork error: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	log.Tracef("mined work length is: %v", minedWork)

	data.MinedWork = minedWork

	resp, err := ui.hub.FetchAccountStats(address)
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchAccountStats error: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	data.AccountStats = resp

	ui.renderTemplate(w, r, "index", data)
}
