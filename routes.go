package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"

	"dnldd/dcrpool/database"
	"dnldd/dcrpool/ws"
)

// errParamNotFound is returned when a provided parameter key does not map
// to any value.
func errParamNotFound(key string) error {
	return fmt.Errorf("associated parameter for key '%v' not found", key)
}

// setupRoutes configures the accessible routes of the mining pool.
func (p *MiningPool) setupRoutes() {
	p.router.HandleFunc("/account/create", p.handleCreateAccount)
	p.router.HandleFunc("/account/{id}/username", p.handleUpdateUsername)
	p.router.HandleFunc("/account/{id}/address", p.handleUpdateAddress)
	p.router.HandleFunc("/account/{id}/password", p.handleUpdatePassword)
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

// respondWithStatusCode responds with only a status code to a request.
func respondWithStatusCode(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(code)
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

// handleCreateAccount handles account creation.
func (p *MiningPool) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest,
			fmt.Errorf("Failed to read request body"))
		return
	}
	if len(body) == 0 {
		respondWithError(w, http.StatusBadRequest,
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

	username, ok := payload["username"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("username"))
		return
	}
	address, ok := payload["address"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("address"))
		return
	}
	password, ok := payload["password"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("password"))
		return
	}

	// Verify the address provided is a valid decred address.

	exists, err := database.IndexExists(p.db, database.UsernameIdxBkt,
		[]byte(strings.ToLower(username)))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
		return
	}
	if exists {
		respondWithError(w,
			http.StatusBadRequest,
			fmt.Errorf("'%s' is registered to another account", username))
		return
	}

	account, err := NewAccount(username, address, password)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
	}
	err = account.Create(p.db)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
	}
	err = database.UpdateIndex(p.db, database.UsernameIdxBkt,
		[]byte(account.UUID), []byte(account.Username))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
	}

	respondWithJSON(w, http.StatusCreated, account)
}

// handleUpdateUsername handles username updates.
func (p *MiningPool) handleUpdateUsername(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id := params["id"]

	account, err := GetAccount(p.db, []byte(id))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest,
			fmt.Errorf("Failed to read request body"))
		return
	}
	if len(body) == 0 {
		respondWithError(w, http.StatusBadRequest,
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

	err = bcrypt.CompareHashAndPassword([]byte(account.Password),
		[]byte(password))
	if err != nil {
		respondWithError(w, http.StatusBadRequest,
			errors.New("password is incorrect"))
		return
	}

	account.Username = username
	account.Update(p.db)

	respondWithJSON(w, http.StatusOK, account)
}

// handleUpdateAddress handles address updates.
func (p *MiningPool) handleUpdateAddress(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id := params["id"]

	account, err := GetAccount(p.db, []byte(id))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest,
			fmt.Errorf("Failed to read request body"))
		return
	}
	if len(body) == 0 {
		respondWithError(w, http.StatusBadRequest,
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

	address, ok := payload["address"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("address"))
		return
	}
	password, ok := payload["password"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("password"))
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(account.Password),
		[]byte(password))
	if err != nil {
		respondWithError(w, http.StatusBadRequest,
			errors.New("password is incorrect"))
		return
	}

	account.Address = address
	account.Update(p.db)

	respondWithJSON(w, http.StatusOK, account)
}

// handleUpdateAddress handles address updates.
func (p *MiningPool) handleUpdatePassword(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	id := params["id"]

	account, err := GetAccount(p.db, []byte(id))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest,
			fmt.Errorf("Failed to read request body"))
		return
	}
	if len(body) == 0 {
		respondWithError(w, http.StatusBadRequest,
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

	password, ok := payload["password"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("password"))
		return
	}
	newPassword, ok := payload["newpassword"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("newpassword"))
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(account.Password),
		[]byte(password))
	if err != nil {
		respondWithError(w, http.StatusBadRequest,
			errors.New("old password is incorrect"))
		return
	}

	hashedPass, err := bcryptHash(newPassword)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err)
		return
	}

	account.Password = string(hashedPass)
	account.Update(p.db)

	respondWithStatusCode(w, http.StatusOK)
}
