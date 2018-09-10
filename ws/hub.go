package ws

import (
	"net/http"

	"github.com/coreos/bbolt"

	"dnldd/dcrpool/limiter"
)

// Hub maintains the set of active clients and facilitates message broadcasting
// to all active clients.
type Hub struct {
	bolt      *bolt.DB
	httpc     *http.Client
	limiter   *limiter.RateLimiter
	Broadcast chan Message
	ConnCount uint64
}

// NewHub initializes a hub.
func NewHub(bolt *bolt.DB, httpc *http.Client, limiter *limiter.RateLimiter) *Hub {
	return &Hub{
		bolt:      bolt,
		httpc:     httpc,
		limiter:   limiter,
		Broadcast: make(chan Message),
		ConnCount: 0,
	}
}

// Close terminates all connected clients to the hub.
func (h *Hub) Close() {
	h.Broadcast <- nil
}
