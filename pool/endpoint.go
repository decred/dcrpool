// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

// endpoint represents a stratum endpoint.
type endpoint struct {
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

// newEndpoint creates an endpoint instance.
func newEndpoint(hub *Hub, port uint32, miner string) (*endpoint, error) {
	endpoint := &endpoint{
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
		desc := fmt.Sprintf("pool difficulty data not found for "+
			"%s miner", miner)
		return nil, MakeError(ErrDifficultyNotFound, desc, nil)
	}

	endpoint.diffData = diffData

	return endpoint, nil
}

// listen sets up a listener for incoming client connections on the endpoint.
// It must be run as a goroutine.
func (e *endpoint) listen() {
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", "0.0.0.0", e.port))
	if err != nil {
		log.Errorf("unable to listen on tcp address: %v", err)
		return
	}

	e.listener = listener
	defer e.listener.Close()
	log.Tracef("Listening on %v for %v", e.port, e.miner)

	for {
		conn, err := e.listener.Accept()
		if err != nil {
			log.Tracef("unable to accept client connection for "+
				"%s endpoint: %v", e.miner, err)
			return
		}

		e.connCh <- conn
	}
}

// connect creates new pool clients from established connections.
// It must be run as a goroutine.
func (e *endpoint) connect(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			e.clientsMtx.Lock()
			for _, client := range e.clients {
				client.cancel()
			}
			e.clientsMtx.Unlock()
			e.wg.Wait()

			e.hub.wg.Done()
			return

		case conn := <-e.connCh:
			addr := conn.RemoteAddr()
			tcpAddr, err := net.ResolveTCPAddr(addr.Network(), addr.String())
			if err != nil {
				log.Errorf("unable to parse tcp addresss: %v", err)
				continue
			}

			host := tcpAddr.IP.String()

			e.hub.connectionsMtx.RLock()
			connections := e.hub.connections[host]
			e.hub.connectionsMtx.RUnlock()

			if connections >= e.hub.cfg.MaxConnectionsPerHost {
				log.Tracef("exceeded maximum connections "+
					"allowed per host (%d) for %s",
					e.hub.cfg.MaxConnectionsPerHost, host)
				continue
			}

			client, err := NewClient(conn, e, tcpAddr)
			if err != nil {
				log.Errorf("unable to create client: %v", err)
				continue
			}

			e.clientsMtx.Lock()
			e.clients[client.generateID()] = client
			e.clientsMtx.Unlock()

			e.hub.connectionsMtx.Lock()
			e.hub.connections[host]++
			e.hub.connectionsMtx.Unlock()

			atomic.AddInt32(&e.hub.clients, 1)

			go client.run(client.ctx)
		}
	}
}

// removeClient removes a disconnected pool client from its associated endpoint.
func (e *endpoint) removeClient(c *Client) {
	e.clientsMtx.Lock()
	id := c.generateID()
	delete(e.clients, id)
	atomic.AddInt32(&e.hub.clients, -1)
	log.Tracef("Client (%s) removed.", id)
	e.clientsMtx.Unlock()
}
