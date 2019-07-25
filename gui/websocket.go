package gui

import (
	"github.com/decred/dcrpool/pool"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var clients = make(map[*websocket.Conn]bool) // connected clients
var broadcast = make(chan Message)           // broadcast channel

// Configure the upgrader
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Message struct {
	PoolHashRate      string      `json:"poolhashrate"`
	LastWorkHeight    uint32      `json:"lastworkheight"`
	LastPaymentHeight uint32      `json:"lastpaymentheight"`
	WorkQuotas        []WorkQuota `json:"workquotas"`
	MinedWork         []MinedWork `json:"minedblocks"`
}

type WorkQuota struct {
	AccountID string `json:"accountid"`
	Percent   string `json:"percent"`
}

type MinedWork struct {
	BlockHeight uint32 `json:"blockheight"`
	BlockURL    string `json:"blockurl"`
	MinedBy     string `json:"minedby"`
	Miner       string `json:"miner"`
}

const socketRefreshRate = 5 * time.Second

func (ui *GUI) RegisterWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade GET request to a websocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("websocket error: %v", err)
	}

	// Register our new client
	clients[ws] = true
}

func (ui *GUI) SendUpdatedValues(stats *pool.Stats, quotas []pool.Quota) {
	msg := Message{
		PoolHashRate:      hashString(stats.PoolHashRate),
		LastWorkHeight:    stats.LastWorkHeight,
		LastPaymentHeight: stats.LastPaymentHeight,
	}

	for _, quota := range quotas {
		msg.WorkQuotas = append(msg.WorkQuotas, WorkQuota{
			AccountID: truncateAccountID(quota.AccountID),
			Percent:   ratToPercent(quota.Percentage),
		})
	}

	for _, block := range stats.MinedWork {
		msg.MinedWork = append(msg.MinedWork, MinedWork{
			BlockHeight: block.Height,
			BlockURL:    blockURL(ui.cfg.BlockExplorerURL, block.Height),
			MinedBy:     truncateAccountID(block.MinedBy),
			Miner:       block.Miner,
		})
	}

	// Send it out to every client that is currently connected
	for client := range clients {
		err := client.WriteJSON(msg)
		if err != nil {
			log.Errorf("websocket error: %v", err)
			client.Close()
			delete(clients, client)
		}
	}
}
