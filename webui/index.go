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

	UserStats *network.UserStats
	Address   string
	Admin     bool
}

func GetIndex(w http.ResponseWriter, r *http.Request) {
	poolStats, err := hub.FetchPoolStats()
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchPoolStats error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	reverseSlice(poolStats.MinedWork)

	poolHashRate, cHash := hub.FetchHash()

	data := indexData{
		PoolStats:    poolStats,
		HashRate:     poolHashRate,
		CHash:        cHash,
		SoloPoolMode: hub.SoloPoolMode(),
		Admin:        false,
	}

	address := r.FormValue("address")

	data.Address = address

	if address == "" {
		renderTemplate(w, r, "index", data)
		return
	}

	resp, err := hub.FetchUserStats(address)
	if err != nil {
		log.Error(err)
		http.Error(w, "FetchUserStats error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	reverseSlice(resp.MinedWork)
	data.UserStats = resp

	renderTemplate(w, r, "index", data)
}

func reverseSlice(slice []*network.AcceptedWork) {
	for i := len(slice)/2 - 1; i >= 0; i-- {
		opp := len(slice) - 1 - i
		slice[i], slice[opp] = slice[opp], slice[i]
	}
}
