// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gui

import (
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// WebsocketServer maintains a list of connected websocket clients (protected by
// a mutex), and is able to send payloads to them. It is used for sending
// updated pool stats to connected GUI clients.
type WebsocketServer struct {
	clients    map[*websocket.Conn]bool
	clientsMtx sync.Mutex
	upgrader   websocket.Upgrader
}

// NewWebsocketServer returns an initialized websocket server.
func NewWebsocketServer() *WebsocketServer {
	return &WebsocketServer{
		clients:  make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{},
	}
}

// payload represents a websocket update message.
type payload struct {
	PoolHashRate      string `json:"poolhashrate,omitempty"`
	LastWorkHeight    uint32 `json:"lastworkheight,omitempty"`
	LastPaymentHeight uint32 `json:"lastpaymentheight,omitempty"`
}

// registerClient is the handler for "GET /ws". It updates the HTTP request
// to a websocket and adds the caller to a list of connected clients.
func (s *WebsocketServer) registerClient(w http.ResponseWriter, r *http.Request) {
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("ws.registerClient error: %v", err)
		return
	}
	s.clientsMtx.Lock()
	s.clients[ws] = true
	s.clientsMtx.Unlock()
}

// send sends the provided payload to all connected websocket clients.
func (s *WebsocketServer) send(payload payload) {
	s.clientsMtx.Lock()
	for client := range s.clients {
		err := client.WriteJSON(payload)
		if err != nil {
			// "broken pipe" indicates the client has disconnected.
			// We don't need to log an error in this case.
			if !strings.Contains(err.Error(), "write: broken pipe") {
				log.Errorf("ws.send: error on client %s: %v", client.LocalAddr(), err)
			}
			client.Close()
			delete(s.clients, client)
		}
	}
	s.clientsMtx.Unlock()
}
