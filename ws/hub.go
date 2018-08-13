package ws

import (
	"net/http"
	"sync"

	"github.com/coreos/bbolt"
	"github.com/gorilla/websocket"
)

// Hub maintains the set of active clients and facilitates message broadcasting
// to all active clients.
type Hub struct {
	bolt  *bolt.DB
	httpc *http.Client

	// The fields below handle message processing.
	clients   map[*Client]bool
	broadcast chan Message
	close     chan *Client
	mutex     sync.Mutex
}

// NewHub initializes a hub.
func NewHub(bolt *bolt.DB, httpc *http.Client) *Hub {
	return &Hub{
		bolt:      bolt,
		httpc:     httpc,
		broadcast: make(chan Message),
		clients:   make(map[*Client]bool),
		close:     make(chan *Client),
		mutex:     sync.Mutex{},
	}
}

// AddClient adds a client websocket client to the hub.
func (h *Hub) AddClient(c *Client) {
	h.mutex.Lock()
	h.clients[c] = true
	h.mutex.Unlock()
}

// Close terminates all connected clients to the hub.
func (h *Hub) Close() {
	h.mutex.Lock()
	for client := range h.clients {
		client.ws.WriteMessage(websocket.CloseMessage, []byte{})
		close(client.ch)
		delete(h.clients, client)
	}
	h.mutex.Unlock()
}

func (h *Hub) run() {
	for {
		select {
		case message := <-h.broadcast:
			h.mutex.Lock()
			for client := range h.clients {
				client.ch <- message
			}
			h.mutex.Unlock()
		case client := <-h.close:
			h.mutex.Lock()
			client.remove()
			h.mutex.Unlock()
		}
	}
}
