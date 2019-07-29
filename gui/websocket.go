package gui

import (
	"net/http"

	"github.com/gorilla/websocket"
)

var clients = make(map[*websocket.Conn]bool)
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

func (ui *GUI) RegisterWebSocket(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("websocket error: %v", err)
		return
	}

	clients[ws] = true
}

// updateWS sends updates to all connected websocket clients.
func (ui *GUI) updateWS() {
	workData := make([]minedWork, 0)
	ui.minedWorkMtx.Lock()
	for _, block := range ui.minedWork {
		workData = append(workData, minedWork{
			BlockHeight: block.Height,
			BlockURL:    blockURL(ui.cfg.BlockExplorerURL, block.Height),
			MinedBy:     truncateAccountID(block.MinedBy),
			Miner:       block.Miner,
		})
	}
	ui.minedWorkMtx.Unlock()

	quotaData := make([]workQuota, 0)
	ui.workQuotasMtx.Lock()
	for _, quota := range ui.workQuotas {
		quotaData = append(quotaData, workQuota{
			AccountID: truncateAccountID(quota.AccountID),
			Percent:   ratToPercent(quota.Percentage),
		})
	}
	ui.workQuotasMtx.Unlock()

	ui.poolHashMtx.Lock()
	poolHash := hashString(ui.poolHash)
	ui.poolHashMtx.Unlock()

	msg := payload{
		LastWorkHeight:    ui.hub.FetchLastWorkHeight(),
		LastPaymentHeight: ui.hub.FetchLastPaymentHeight(),
		PoolHashRate:      poolHash,
		WorkQuotas:        quotaData,
		MinedWork:         workData,
	}

	for client := range clients {
		err := client.WriteJSON(msg)
		if err != nil {
			log.Errorf("websocket error: %v", err)
			client.Close()
			delete(clients, client)
		}
	}
}
