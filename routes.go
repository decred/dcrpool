package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

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
	p.router.HandleFunc("/create/account", p.handleCreateAccount).
		Methods(http.MethodPost)
	p.router.HandleFunc("/update/name", p.handleUpdateName).
		Methods(http.MethodPost)
	p.router.HandleFunc("/update/address", p.handleUpdateAddress).
		Methods(http.MethodPost)
	p.router.HandleFunc("/update/pass", p.handleUpdatePass).
		Methods(http.MethodPost)
	p.router.HandleFunc("/ws", p.handleWS)
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

// getBasicAuthorization returns the user credentials in a basic
// authorization header of a request.
func getBasicAuthorization(r *http.Request) (string, string, error) {
	auth := strings.Split(r.Header.Get("Authorization"), " ")
	if len(auth) != 2 || auth[0] != "Basic" {
		return "", "", fmt.Errorf("Authorization type is not Basic")
	}
	payload, _ := base64.StdEncoding.DecodeString(auth[1])
	pair := strings.Split(string(payload), ":")
	if len(pair) != 2 {
		return "", "", fmt.Errorf("Authorization type is not Basic")
	}
	return pair[0], pair[1], nil
}

// handleWS establishes websocket connections with clients.
// endpoint: POST ip:port/ws
// 	Authorization: Basic base64('user:pass')
func (p *MiningPool) handleWS(w http.ResponseWriter, r *http.Request) {
	name, pass, err := getBasicAuthorization(r)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err)
		return
	}

	// Lookup the user account.
	id, err := database.GetIndexValue(p.db, database.NameIdxBkt,
		[]byte(strings.ToLower(name)))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err)
		return
	}

	if id == nil {
		respondWithError(w, http.StatusBadRequest,
			fmt.Errorf("'%s' is not a registered user", name))
		return
	}

	account, err := GetAccount(p.db, id)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err)
		return
	}

	// Authenticate the request.
	err = bcrypt.CompareHashAndPassword([]byte(account.Pass),
		[]byte(pass))
	if err != nil {
		respondWithError(w, http.StatusBadRequest,
			fmt.Errorf("the provided pass for account '%s' is incorrect",
				name))
		return
	}

	// Upgrade the http request to a websocket connection.
	conn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		pLog.Error(err)
		return
	}

	c := ws.NewClient(p.hub, conn, r.RemoteAddr)
	go c.Process(c.Ctx)
	go c.Send(c.Ctx)
}

// limit ensures all incoming requests stay within the rate limit bounds
// defined.
func (p *MiningPool) limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allow := p.limiter.WithinLimit(r.RemoteAddr)
		if !allow {
			http.Error(w, http.StatusText(429), http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleCreateAccount handles account creation.
// endpoint: POST ip:port/create/account
// 	sample payload:
// 		{
// 			"name": "dnldd",
// 			"address": "DsTpf5FoEQHRFE43VJcgsHxBX55s9WAHM78",
// 			"pass": "pass"
// 		}
// 	sample response:
// 		{
// 			"uuid": "19oBmvDBPjIa3Lq7aE9FdoDebhM",
// 			"name": "dnldd",
// 			"address": "DsTpf5FoEQHRFE43VJcgsHxBX55s9WAHM78",
// 			"createdon": 1536186735,
// 			"modifiedon": 0
// 		}
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

	name, ok := payload["name"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("name"))
		return
	}
	address, ok := payload["address"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("address"))
		return
	}
	pass, ok := payload["pass"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("pass"))
		return
	}

	// TODO: Verify the address provided is a valid decred address.

	id, err := database.GetIndexValue(p.db, database.NameIdxBkt,
		[]byte(strings.ToLower(name)))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
		return
	}
	if id != nil {
		respondWithError(w,
			http.StatusBadRequest,
			fmt.Errorf("'%s' is registered to another account", name))
		return
	}

	account, err := NewAccount(name, address, pass)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
		return
	}
	err = account.Create(p.db)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
		return
	}
	err = database.UpdateIndex(p.db, database.NameIdxBkt,
		[]byte(account.Name), []byte(account.UUID))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
		return
	}

	// Sanitize account of sensitive data before responding.
	account.Pass = ""
	respondWithJSON(w, http.StatusCreated, account)
}

// handleUpdateName handles name updates.
// endpoint: POST ip:port/update/name
// 		sample payload:
// 			{
// 				"oldname": "dnldd",
// 				"newname": "einheit",
// 				"pass": "pass"
// 			}
// 		sample response:
// 		{
// 			"uuid": "19oBmvDBPjIa3Lq7aE9FdoDebhM",
// 			"name": "einheit",
// 			"address": "DsTpf5FoEQHRFE43VJcgsHxBX55s9WAHM78",
// 			"createdon": 1536186735,
// 			"modifiedon": 1536189736
// 		}
func (p *MiningPool) handleUpdateName(w http.ResponseWriter, r *http.Request) {
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

	oldName, ok := payload["oldname"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("oldname"))
		return
	}
	newName, ok := payload["newname"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("newname"))
		return
	}
	pass, ok := payload["pass"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("pass"))
		return
	}

	// Fetch the corresponding account of the name.
	id, err := database.GetIndexValue(p.db, database.NameIdxBkt,
		[]byte(strings.ToLower(oldName)))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
		return
	}
	if id == nil {
		respondWithError(w,
			http.StatusBadRequest,
			fmt.Errorf("no account found with name '%s'", oldName))
		return
	}

	account, err := GetAccount(p.db, []byte(id))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err)
		return
	}

	// Authenticate the request.
	err = bcrypt.CompareHashAndPassword([]byte(account.Pass),
		[]byte(pass))
	if err != nil {
		respondWithError(w, http.StatusBadRequest,
			fmt.Errorf("the provided pass for account '%s' is incorrect",
				oldName))
		return
	}

	// Update the account name and index.
	account.Name = newName
	account.ModifiedOn = uint64(time.Now().Unix())
	account.Update(p.db)

	err = database.RemoveIndex(p.db, database.NameIdxBkt, []byte(oldName))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
		return
	}
	err = database.UpdateIndex(p.db, database.NameIdxBkt, []byte(newName),
		[]byte(account.UUID))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
		return
	}

	// Sanitize account of sensitive data before responding.
	account.Pass = ""
	respondWithJSON(w, http.StatusOK, account)
}

// handleUpdateAddress handles address updates.
// endpoint: POST ip:port/update/address
// 		sample payload:
// 			{
// 				"name": "dnldd",
// 				"address": "DsTpf5FoEQHRFE43VJcgsHxBX55s9WAHM78",
// 				"pass": "pass"
// 			}
// 		sample response:
// 		{
// 			"uuid": "19pjXE2EJbF938dVOvj0w2FN5tR",
// 			"name": "dnldd",
// 			"address": "DsTpf5FoEQHRFE43VJcgsHxBX55s9WAHM78",
// 			"createdon": 1536233973,
// 			"modifiedon": 1536233991
// 		}
func (p *MiningPool) handleUpdateAddress(w http.ResponseWriter, r *http.Request) {
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

	name, ok := payload["name"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("name"))
		return
	}
	address, ok := payload["address"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("address"))
		return
	}
	pass, ok := payload["pass"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("pass"))
		return
	}

	// Fetch the corresponding account of the name.
	id, err := database.GetIndexValue(p.db, database.NameIdxBkt,
		[]byte(strings.ToLower(name)))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
		return
	}
	if id == nil {
		respondWithError(w,
			http.StatusBadRequest,
			fmt.Errorf("no account found with name '%s'", name))
		return
	}

	account, err := GetAccount(p.db, []byte(id))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err)
		return
	}

	// Authenticate the request.
	err = bcrypt.CompareHashAndPassword([]byte(account.Pass),
		[]byte(pass))
	if err != nil {
		respondWithError(w, http.StatusBadRequest,
			fmt.Errorf("the provided pass for account '%s' is incorrect",
				name))
		return
	}

	// Update the account address.
	account.Address = address
	account.ModifiedOn = uint64(time.Now().Unix())
	account.Update(p.db)

	// Sanitize account of sensitive data before responding.
	account.Pass = ""
	respondWithJSON(w, http.StatusOK, account)
}

// handleUpdatePass handles password updates.
// endpoint: POST ip:port/update/pass
// 		sample payload:
// 			{
// 				"name": "dnldd",
// 				"oldpass": "pass",
// 				"newpass": "h3ll0"
// 			}
// 		sample response: Status OK
func (p *MiningPool) handleUpdatePass(w http.ResponseWriter, r *http.Request) {
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

	name, ok := payload["name"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("name"))
		return
	}
	oldPass, ok := payload["oldpass"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("oldpass"))
		return
	}
	newPass, ok := payload["newpass"].(string)
	if !ok {
		respondWithError(w, http.StatusBadRequest, errParamNotFound("newpass"))
		return
	}

	// Fetch the corresponding account of the name.
	id, err := database.GetIndexValue(p.db, database.NameIdxBkt,
		[]byte(strings.ToLower(name)))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err)
		return
	}
	if id == nil {
		respondWithError(w,
			http.StatusBadRequest,
			fmt.Errorf("no account found with name '%s'", name))
		return
	}

	account, err := GetAccount(p.db, []byte(id))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err)
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(account.Pass),
		[]byte(oldPass))
	if err != nil {
		respondWithError(w, http.StatusBadRequest,
			fmt.Errorf("the provided pass for account '%s' is incorrect", name))
		return
	}

	hashedPass, err := bcryptHash(newPass)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err)
		return
	}

	// Update the account address.
	account.Pass = string(hashedPass)
	account.ModifiedOn = uint64(time.Now().Unix())
	account.Update(p.db)

	respondWithStatusCode(w, http.StatusOK)
}
