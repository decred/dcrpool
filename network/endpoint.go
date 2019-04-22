// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

// Endpoint represents a stratum endpoint.
type Endpoint struct {
	port       uint32
	diffData   *DifficultyData
	miner      string
	listener   net.Listener
	hub        *Hub
	clients    map[string]*Client
	clientsMtx sync.Mutex
	connCh     chan net.Conn
	wg         sync.WaitGroup
}

// NewEndpoint creates an endpoint instance.
func NewEndpoint(hub *Hub, port uint32, miner string) (*Endpoint, error) {
	endpoint := &Endpoint{
		port:    port,
		hub:     hub,
		miner:   miner,
		clients: make(map[string]*Client),
		connCh:  make(chan net.Conn),
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

// listen sets up a listener for incoming client connections on the endpoint.
// It must be run as a goroutine.
func (e *Endpoint) listen() {
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", "0.0.0.0", e.port))
	if err != nil {
		log.Errorf("unable to listen on tcp address: %v", err)
		return
	}

	e.listener = listener
	defer e.listener.Close()
	log.Infof("Listening on %v for %v", e.port, e.miner)

	for {
		conn, err := e.listener.Accept()
		if err != nil {
			log.Tracef("unable to accept connection: %v for endpoint %v",
				err, e.port)
			return
		}

		e.connCh <- conn
	}
}

// connect creates new pool clients from established connections.
// It must be run as a goroutine.
func (e *Endpoint) connect(ctx context.Context) {
	log.Tracef("Started connection handler for %v clients.", e.miner)

	for {
		select {
		case <-ctx.Done():
			e.clientsMtx.Lock()
			for _, client := range e.clients {
				log.Tracef("Terminating (%v) client.", client.generateID())
				client.cancel()
			}
			e.clientsMtx.Unlock()
			e.wg.Wait()

			log.Tracef("Connection handler for %v clients done.", e.miner)
			e.hub.wg.Done()
			return

		case conn := <-e.connCh:
			client := NewClient(conn, e, conn.RemoteAddr().String())
			e.clientsMtx.Lock()
			e.clients[client.generateID()] = client
			e.clientsMtx.Unlock()

			updated := atomic.AddUint32(&e.hub.clients, 1)
			atomic.StoreUint32(&e.hub.clients, updated)

			go client.run(client.ctx)
		}

	}
}

// RemoveClient removes a disconnected pool client from its associated endpoint.
func (e *Endpoint) RemoveClient(c *Client) {
	e.clientsMtx.Lock()
	id := c.generateID()
	delete(e.clients, id)
	log.Tracef("Client (%s) removed.", id)
	e.clientsMtx.Unlock()
}
