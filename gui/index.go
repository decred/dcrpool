package gui

import (
	"net/http"
	"reflect"

	"github.com/decred/dcrpool/network"
)

type indexData struct {
	PoolStats    *network.PoolStats
	HashRate     string
	HashSet      []network.ClientHash
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

	resp, err := ui.hub.FetchAccountStats(address)
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchAccountStats error: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	data.AccountStats = resp

	reverseSlice(data.AccountStats.MinedWork)
	reverseSlice(data.AccountStats.Payments)

	ui.renderTemplate(w, r, "index", data)
}

func reverseSlice(s interface{}) {
	size := reflect.ValueOf(s).Len()
	swap := reflect.Swapper(s)
	for i, j := 0, size-1; i < j; i, j = i+1, j-1 {
		swap(i, j)
	}
}
