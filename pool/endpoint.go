// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg/v2"
)

type EndpointConfig struct {
	// ActiveNet represents the active network being mined on.
	ActiveNet *chaincfg.Params
	// DB represents the pool database.
	DB *bolt.DB
	// SoloPool represents the solo pool mining mode.
	SoloPool bool
	// Blake256Pad represents the extra padding needed for work
	// submissions over the getwork RPC.
	Blake256Pad []byte
	// NonceIterations returns the possible header nonce iterations.
	NonceIterations float64
	// MaxConnectionsPerHost represents the maximum number of connections
	// allowed per host.
	MaxConnectionsPerHost uint32
	// HubWg represents the hub's waitgroup.
	HubWg *sync.WaitGroup
	// SubmitWork sends solved block data to the consensus daemon.
	SubmitWork func(*string) (bool, error)
	// FetchCurrentWork returns the current work of the pool.
	FetchCurrentWork func() string
	// WithinLimit returns if a client is within its request limits.
	WithinLimit func(string, int) bool
	// AddConnection records a new client connection.
	AddConnection func(string)
	// RemoveConnection removes a client connection.
	RemoveConnection func(string)
	// FetchHostConnections returns the host connection for the provided host.
	FetchHostConnections func(string) uint32
}

// Endpoint represents a stratum endpoint.
type Endpoint struct {
	miner      string
	port       uint32
	diffInfo   *DifficultyInfo
	connCh     chan net.Conn
	listener   net.Listener
	cfg        *EndpointConfig
	clients    map[string]*Client
	clientsMtx sync.Mutex
	wg         sync.WaitGroup
}

// NewEndpoint creates an new miner endpoint.
func NewEndpoint(eCfg *EndpointConfig, diffInfo *DifficultyInfo, port uint32, miner string) (*Endpoint, error) {
	endpoint := &Endpoint{
		port:     port,
		miner:    miner,
		diffInfo: diffInfo,
		cfg:      eCfg,
		clients:  make(map[string]*Client),
		connCh:   make(chan net.Conn),
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", "0.0.0.0", endpoint.port))
	if err != nil {
		return nil, err
	}
	endpoint.listener = listener
	return endpoint, nil
}

// removeClient removes a disconnected pool client from its associated endpoint.
func (e *Endpoint) removeClient(c *Client) {
	e.clientsMtx.Lock()
	delete(e.clients, c.id)
	e.clientsMtx.Unlock()
	e.cfg.RemoveConnection(c.addr.IP.String())
}

// listen accepts incoming client connections on the endpoint.
// It must be run as a goroutine.
func (e *Endpoint) listen() {
	log.Infof("%s listening on :%d", e.miner, e.port)
	for {
		conn, err := e.listener.Accept()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok {
				if opErr.Op == "accept" {
					if strings.Contains(opErr.Err.Error(),
						"use of closed network connection") {
						return
					}
				}
			}
			log.Errorf("unable to accept client connection for "+
				"%s endpoint: %v", e.miner, err)
			return
		}
		e.connCh <- conn
	}
}

// connect creates new pool clients from established connections.
// It must be run as a goroutine.
func (e *Endpoint) connect(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			e.listener.Close()
			e.wg.Done()
			e.clientsMtx.Lock()
			for _, client := range e.clients {
				client.cancel()
			}
			e.clientsMtx.Unlock()
			e.cfg.HubWg.Done()
			return

		case conn := <-e.connCh:
			addr := conn.RemoteAddr()
			tcpAddr, err := net.ResolveTCPAddr(addr.Network(), addr.String())
			if err != nil {
				log.Errorf("unable to parse tcp addresss: %v", err)
				continue
			}
			host := tcpAddr.IP.String()
			connCount := e.cfg.FetchHostConnections(host)
			if connCount >= e.cfg.MaxConnectionsPerHost {
				log.Errorf("exceeded maximum connections allowed per"+
					" host %d for %s", e.cfg.MaxConnectionsPerHost, host)
				conn.Close()
				continue
			}
			cCfg := &ClientConfig{
				ActiveNet:       e.cfg.ActiveNet,
				DB:              e.cfg.DB,
				Blake256Pad:     e.cfg.Blake256Pad,
				NonceIterations: e.cfg.NonceIterations,
				FetchMiner: func() string {
					return e.miner
				},
				DifficultyInfo:   e.diffInfo,
				EndpointWg:       &e.wg,
				RemoveClient:     e.removeClient,
				SubmitWork:       e.cfg.SubmitWork,
				FetchCurrentWork: e.cfg.FetchCurrentWork,
				WithinLimit:      e.cfg.WithinLimit,
			}
			client, err := NewClient(conn, tcpAddr, cCfg)
			if err != nil {
				log.Errorf("unable to create client: %v", err)
				continue
			}
			e.clientsMtx.Lock()
			e.clients[client.id] = client
			e.clientsMtx.Unlock()
			e.cfg.AddConnection(host)
			go client.run(client.ctx)
		}
	}
}

// run handles the lifecycle of all endpoint related processes.
// This should be run as a goroutine.
func (e *Endpoint) run(ctx context.Context) {
	e.wg.Add(1)
	go e.listen()
	go e.connect(ctx)
	e.wg.Wait()
}
