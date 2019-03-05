// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"fmt"
	"net"
	"sync"
)

const (
	// TCP represents the tcp network protocol.
	TCP = "tcp"

	// MaxMessageSize represents the maximum size of a transmitted message,
	// in bytes.
	MaxMessageSize = 511
)

// Endpoint represents a stratum endpoint.
type Endpoint struct {
	port       uint32
	diffData   *DifficultyData
	miner      string
	listener   net.Listener
	hub        *Hub
	clients    []*Client
	clientsMtx sync.Mutex
}

// NewEndpoint creates an endpoint instance.
func NewEndpoint(hub *Hub, port uint32, miner string) (*Endpoint, error) {
	endpoint := &Endpoint{
		port:  port,
		hub:   hub,
		miner: miner,
	}

	hub.poolDiffMtx.Lock()
	diffData := hub.poolDiff[miner]
	hub.poolDiffMtx.Unlock()
	if diffData == nil {
		return nil, fmt.Errorf("pool difficulty data not found for miner (%s)",
			miner)
	}

	endpoint.diffData = diffData

	return endpoint, nil
}

// Listen sets up a listener for incoming messages on the endpoint. It must be
// run as a goroutine.
func (e *Endpoint) Listen() {
	listener, err := net.Listen(TCP, fmt.Sprintf("%s:%d", "0.0.0.0", e.port))
	if err != nil {
		log.Errorf("Failed to listen on tcp address: %v", err)
		return
	}

	e.listener = listener
	defer e.listener.Close()

	log.Infof("Listening on %v for %v", e.port, e.miner)

	for {
		select {
		case <-e.hub.ctx.Done():
			e.clientsMtx.Lock()
			for _, client := range e.clients {
				client.cancel()
			}
			e.clientsMtx.Unlock()

			log.Infof("Listener for %v on %v done", e.miner, e.port)
			return

		default:
			// Non-blocking receive fallthrough.
		}

		conn, err := e.listener.Accept()
		if err != nil {
			log.Tracef("Failed to accept connection: %v for endpoint %v",
				err, e.port)
			return
		}

		client := NewClient(conn, e, conn.RemoteAddr().String())
		e.clientsMtx.Lock()
		e.clients = append(e.clients, client)
		e.clientsMtx.Unlock()

		go client.Listen()
		go client.Send()
	}
}

// RemoveClient removes a disconnected pool client from its associated endpoint.
func (e *Endpoint) RemoveClient(c *Client) {
	e.clientsMtx.Lock()
	for idx := 0; idx < len(e.clients); idx++ {
		if c.ip == e.clients[idx].ip {
			copy(e.clients[idx:], e.clients[idx+1:])
			e.clients[len(e.clients)-1] = nil
			e.clients = e.clients[:len(e.clients)-1]
			break
		}
	}
	e.clientsMtx.Unlock()
}
