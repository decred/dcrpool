package gui

import (
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

var clients = make(map[*websocket.Conn]bool)
var clientsMtx sync.Mutex
var upgrader = websocket.Upgrader{}

// payload represents a websocket update message.
type payload struct {
	PoolHashRate      string      `json:"poolhashrate"`
	LastWorkHeight    uint32      `json:"lastworkheight"`
	LastPaymentHeight uint32      `json:"lastpaymentheight"`
	WorkQuotas        []workQuota `json:"workquotas"`
}

// workQuota represents dividend garnered by pool accounts through work
// contributed.
type workQuota struct {
	AccountID string `json:"accountid"`
	Percent   string `json:"percent"`
}

// registerWebSocket is the handler for "GET /ws". It updates the HTTP request
// to a websocket and adds the caller to a list of connected clients.
func (ui *GUI) registerWebSocket(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("registerWebSocket error: %v", err)
		return
	}
	clientsMtx.Lock()
	clients[ws] = true
	clientsMtx.Unlock()
}

// updateWebSocket sends updates to all connected websocket clients.
func (ui *GUI) updateWebSocket() {
	ui.poolHashMtx.RLock()
	poolHash := ui.poolHash
	ui.poolHashMtx.RUnlock()
	ui.workQuotasMtx.RLock()
	workQuotas := append(ui.workQuotas[:0:0], ui.workQuotas...)
	ui.workQuotasMtx.RUnlock()
	msg := payload{
		LastWorkHeight:    ui.cfg.FetchLastWorkHeight(),
		LastPaymentHeight: ui.cfg.FetchLastPaymentHeight(),
		PoolHashRate:      poolHash,
		WorkQuotas:        workQuotas,
	}
	clientsMtx.Lock()
	for client := range clients {
		err := client.WriteJSON(msg)
		if err != nil {
			// "broken pipe" indicates the client has disconnected.
			// We don't need to log an error in this case.
			if !strings.Contains(err.Error(), "write: broken pipe") {
				log.Errorf("updateWebSocket: error on client %s: %v", client.LocalAddr(), err)
			}
			client.Close()
			delete(clients, client)
		}
	}
	clientsMtx.Unlock()
}
