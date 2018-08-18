package main

import (
	"dnldd/dcrpool/database"
	"dnldd/dcrpool/mgmt"
	"dnldd/dcrpool/ws"
	"einheit/eana/util"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

// errParamNotFound is returned when a provided parameter key does not map
// to any value.
func errParamNotFound(key string) error {
	return fmt.Errorf("associated parameter for key '%v' not found", key)
}

// setupRoutes configures the accessible routes of the mining pool.
func (p *MiningPool) setupRoutes() {
	p.router.HandleFunc("/register", p.handleRegistration)
	p.router.HandleFunc("/ws", p.handleWebsockets)
}

// respondWithError writes a JSON error message to a request.
func respondWithError(w http.ResponseWriter, code int, err error) {
	respondWithJSON(w, code, map[string]string{"error": err.Error()})
}

// respondWithJSON writes a JSON payload to a request.
func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

// handleWebsockets establishes websocket connections with clients and handles
// subsequent requests.
func (p *MiningPool) handleWebsockets(w http.ResponseWriter, r *http.Request) {
	// Upgrade the http request to a websocket connection.
	socket, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		mpLog.Error(err)
		return
	}

	c := ws.NewClient(p.hub, socket)
	p.hub.AddClient(c)
	go c.Process()
	go c.Send()
}

// handleRegistration handles new account registration.
func (p *MiningPool) handleRegistration(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		util.RespondWithError(w, http.StatusBadRequest,
			fmt.Errorf("Failed to read request body"))
		return
	}
	if len(body) == 0 {
		util.RespondWithError(w, http.StatusBadRequest,
			fmt.Errorf("Invalid request body"))
		return
	}

	var payload map[string]interface{}
	err = json.Unmarshal(body, &payload)
	if err != nil {
		respondWithError(w, http.StatusBadRequest,
			fmt.Errorf("Request body is invalid json: %v", err))
		return
	}

	email, ok := payload["email"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("email"))
		return
	}
	username, ok := payload["username"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("username"))
		return
	}
	password, ok := payload["password"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("password"))
		return
	}

	// Ensure the email and username provided have not already been indexed.
	emailExists, err := database.IndexExists(p.db, database.EmailIdxBkt,
		[]byte(strings.ToLower(email)))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
		return
	}

	if emailExists {
		respondWithError(w,
			http.StatusBadRequest,
			fmt.Errorf("the email provided ('%s') is registered"+
				" to another account", email))
		return
	}

	usernameExists, err := database.IndexExists(p.db, database.UsernameIdxBkt,
		[]byte(strings.ToLower(username)))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
		return
	}

	if usernameExists {
		respondWithError(w,
			http.StatusBadRequest,
			fmt.Errorf("the username provided ('%s') is registered"+
				" to another account", username))
		return
	}

	account, err := mgmt.NewAccount(email, username, password)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
	}

	err = account.Create(p.db)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
	}

	// Send account activation email.

	respondWithJSON(w, http.StatusCreated, account)
}
