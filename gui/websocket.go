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
	PoolHashRate      string `json:"poolhashrate"`
	LastWorkHeight    uint32 `json:"lastworkheight"`
	LastPaymentHeight uint32 `json:"lastpaymentheight"`
}

const RefreshRate = 5 * time.Second

func (ui *GUI) RegisterWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade GET request to a websocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("websocket error: %v", err)
	}

	// Register our new client
	clients[ws] = true
}

func (ui *GUI) SendUpdatedValues(stats *pool.Stats) {
	msg := Message{
		PoolHashRate:      hashString(stats.PoolHashRate),
		LastWorkHeight:    stats.LastWorkHeight,
		LastPaymentHeight: stats.LastPaymentHeight,
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
