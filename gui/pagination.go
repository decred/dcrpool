// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/decred/dcrpool/pool"
)

type minedWorkPayload struct {
	Blocks []minedWork `json:"blocks"`
	Count  int         `json:"count"`
}

// PaginatedBlocks is the handler for "GET /blocks". It will use
// parameters pageNumber and pageSize to prepare a json payload describing
// blocks mined by the pool, as well as the total count of all confirmed blocks.
func (ui *GUI) PaginatedBlocks(w http.ResponseWriter, r *http.Request) {
	session, err := getSession(r, ui.cookieStore)
	if err != nil {
		log.Errorf("getSession error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if !ui.cfg.WithinLimit(session.ID, pool.APIClient) {
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}

	// Parse request parameters
	pageNumber, err := strconv.Atoi(r.FormValue("pageNumber"))
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	pageSize, err := strconv.Atoi(r.FormValue("pageSize"))
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	offset := (pageNumber - 1) * pageSize
	lastBlock := offset + pageSize

	// Get the requested blocks from the cache.
	allWork := ui.cache.getMinedWork()
	count := len(allWork)
	if lastBlock > count {
		lastBlock = count
	}
	requestedBlocks := allWork[offset:lastBlock]

	// Prepare json response
	payload := minedWorkPayload{
		Count:  count,
		Blocks: requestedBlocks,
	}
	js, err := json.Marshal(payload)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Send json response
	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}

// PaginatedBlocksByAccount is the handler for "GET /blocks_by_account". It will
// use parameters pageNumber, pageSize and accountID to prepare a json payload
// describing blocks mined by the account, as well as the total count of all
// blocks mined by the account.
func (ui *GUI) PaginatedBlocksByAccount(w http.ResponseWriter, r *http.Request) {
	session, err := getSession(r, ui.cookieStore)
	if err != nil {
		log.Errorf("getSession error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if !ui.cfg.WithinLimit(session.ID, pool.APIClient) {
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}

	// Parse request parameters
	pageNumber, err := strconv.Atoi(r.FormValue("pageNumber"))
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	pageSize, err := strconv.Atoi(r.FormValue("pageSize"))
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	accountID := r.FormValue("accountID")

	offset := (pageNumber - 1) * pageSize
	lastBlock := offset + pageSize

	// Get all blocks mined by this account
	work := make([]minedWork, 0)
	allWork := ui.cache.getMinedWork()
	for _, v := range allWork {
		if v.AccountID == accountID {
			work = append(work, v)
		}
	}

	count := len(work)
	if lastBlock > count {
		lastBlock = count
	}
	requestedBlocks := work[offset:lastBlock]

	// Prepare json response
	payload := minedWorkPayload{
		Count:  count,
		Blocks: requestedBlocks,
	}
	js, err := json.Marshal(payload)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Send json response
	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}
