package gui

import (
	"net/http"
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
	MinedWork         []minedWork `json:"minedblocks"`
}

// workQuota represents dividend garnered by pool accounts through work
// contributed.
type workQuota struct {
	AccountID string `json:"accountid"`
	Percent   string `json:"percent"`
}

// minedWork represents a block mined by the pool.
type minedWork struct {
	BlockHeight uint32 `json:"blockheight"`
	BlockURL    string `json:"blockurl"`
	MinedBy     string `json:"minedby"`
	Miner       string `json:"miner"`
}

func (ui *GUI) registerWebSocket(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("websocket error: %v", err)
		return
	}
	clientsMtx.Lock()
	clients[ws] = true
	clientsMtx.Unlock()
}

// updateWS sends updates to all connected websocket clients.
func (ui *GUI) updateWS() {
	ui.poolHashMtx.RLock()
	poolHash := ui.poolHash
	ui.poolHashMtx.RUnlock()
	ui.minedWorkMtx.RLock()
	minedWork := append(ui.minedWork[:0:0], ui.minedWork...)
	ui.minedWorkMtx.RUnlock()
	ui.workQuotasMtx.RLock()
	workQuotas := append(ui.workQuotas[:0:0], ui.workQuotas...)
	ui.workQuotasMtx.RUnlock()
	msg := payload{
		LastWorkHeight:    ui.cfg.FetchLastWorkHeight(),
		LastPaymentHeight: ui.cfg.FetchLastPaymentHeight(),
		PoolHashRate:      poolHash,
		WorkQuotas:        workQuotas,
		MinedWork:         minedWork,
	}
	clientsMtx.Lock()
	for client := range clients {
		err := client.WriteJSON(msg)
		if err != nil {
			log.Errorf("websocket error: %v", err)
			client.Close()
			delete(clients, client)
		}
	}
	clientsMtx.Unlock()
}
