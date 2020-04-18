// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

type rewardQuotasPayload struct {
	RewardQuotas []rewardQuota `json:"rewardquotas"`
	Count        int           `json:"count"`
}

type minedWorkPayload struct {
	Blocks []minedWork `json:"blocks"`
	Count  int         `json:"count"`
}

type clientsPayload struct {
	Clients []client `json:"clients"`
	Count   int      `json:"count"`
}

// getPaginationParams parses the request parameters to find pageNumber and
// pageSize which are required for all paginated data requests. Returns first
// and last, the indices of the first and last items to return.
func getPaginationParams(r *http.Request) (first, last int, err error) {
	pageNumber, err := strconv.Atoi(r.FormValue("pageNumber"))
	if err != nil {
		return 0, 0, err
	}
	pageSize, err := strconv.Atoi(r.FormValue("pageSize"))
	if err != nil {
		return 0, 0, err
	}

	first = (pageNumber - 1) * pageSize
	last = first + pageSize

	return first, last, nil
}

// sendJSONResponse JSON encodes the provided payload and writes it to the
// ResponseWriter. Will send a "500 Internal Server Error" if JSON encoding
// fails.
func sendJSONResponse(w http.ResponseWriter, payload interface{}) {
	js, err := json.Marshal(payload)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}

// PaginatedBlocks is the handler for "GET /blocks". It uses parameters
// pageNumber and pageSize to prepare a json payload describing blocks mined by
// the pool, as well as the total count of all confirmed blocks.
func (ui *GUI) PaginatedBlocks(w http.ResponseWriter, r *http.Request) {
	first, last, err := getPaginationParams(r)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get the requested blocks from the cache.
	allWork := ui.cache.getMinedWork()
	count := len(allWork)
	if last > count {
		last = count
	}
	requestedBlocks := allWork[first:last]

	sendJSONResponse(w, minedWorkPayload{
		Count:  count,
		Blocks: requestedBlocks,
	})
}

// PaginatedRewardQuotas is the handler for "GET /rewardquotas". It uses
// parameters pageNumber and pageSize to prepare a json payload describing
// pending reward payment quotas, as well as the total count of all reward
// quotas.
func (ui *GUI) PaginatedRewardQuotas(w http.ResponseWriter, r *http.Request) {
	first, last, err := getPaginationParams(r)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get the requested rewardQuotas from the cache.
	allRewardQuotas := ui.cache.getRewardQuotas()
	count := len(allRewardQuotas)
	if last > count {
		last = count
	}
	requestedRewardQuotas := allRewardQuotas[first:last]

	sendJSONResponse(w, rewardQuotasPayload{
		Count:        count,
		RewardQuotas: requestedRewardQuotas,
	})
}

// PaginatedBlocksByAccount is the handler for "GET /account/{accountID}/blocks".
// It uses parameters pageNumber, pageSize and accountID to prepare a json
// payload describing blocks mined by the account, as well as the total count of
// all blocks mined by the account.
func (ui *GUI) PaginatedBlocksByAccount(w http.ResponseWriter, r *http.Request) {
	first, last, err := getPaginationParams(r)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	accountID := mux.Vars(r)["accountID"]

	// Get all blocks mined by this account.
	work := make([]minedWork, 0)
	allWork := ui.cache.getMinedWork()
	for _, v := range allWork {
		if v.AccountID == accountID {
			work = append(work, v)
		}
	}

	count := len(work)
	if last > count {
		last = count
	}
	requestedBlocks := work[first:last]

	sendJSONResponse(w, minedWorkPayload{
		Count:  count,
		Blocks: requestedBlocks,
	})
}

// PaginatedClientsByAccount is the handler for "GET /account/{accountID}/clients".
// It uses parameters pageNumber, pageSize and accountID to prepare a json
// payload describing connected mining clients belonging to the account, as well
// as the total count of all connected clients.
func (ui *GUI) PaginatedClientsByAccount(w http.ResponseWriter, r *http.Request) {
	first, last, err := getPaginationParams(r)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	accountID := mux.Vars(r)["accountID"]

	// Get all of this accounts clients.
	allClients := ui.cache.getClients()[accountID]

	count := len(allClients)
	if last > count {
		last = count
	}

	sendJSONResponse(w, clientsPayload{
		Count:   count,
		Clients: allClients[first:last],
	})
}
