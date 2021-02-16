// Copyright (c) 2019-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/v3"
	errs "github.com/decred/dcrpool/errors"
)

// EndpointConfig contains all of the configuration values which should be
// provided when creating a new instance of Endpoint.
type EndpointConfig struct {
	// ActiveNet represents the active network being mined on.
	ActiveNet *chaincfg.Params
	// db represents the pool database.
	db Database
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
	// MaxGenTime represents the share creation target time for the pool.
	MaxGenTime time.Duration
	// HubWg represents the hub's waitgroup.
	HubWg *sync.WaitGroup
	// FetchMinerDifficulty returns the difficulty information for the
	// provided miner if it exists.
	FetchMinerDifficulty func(string) (*DifficultyInfo, error)
	// SubmitWork sends solved block data to the consensus daemon.
	SubmitWork func(context.Context, *string) (bool, error)
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
	// SignalCache sends the provided cache update event to the gui cache.
	SignalCache func(event CacheUpdateEvent)
	// MonitorCycle represents the time monitoring a mining client to access
	// possible upgrades if needed.
	MonitorCycle time.Duration
	// MaxUpgradeTries represents the maximum number of consecutive miner
	// monitoring and upgrade tries.
	MaxUpgradeTries uint32
	// ClientTimeout represents the read/write timeout for the client.
	ClientTimeout time.Duration
}

// connection wraps a client connection and a done channel.
type connection struct {
	Conn net.Conn
	Done chan bool
}

// Endpoint represents a stratum endpoint.
type Endpoint struct {
	listenAddr string
	connCh     chan *connection
	discCh     chan struct{}
	listener   net.Listener
	cfg        *EndpointConfig
	clients    map[string]*Client
	clientsMtx sync.Mutex
	wg         sync.WaitGroup
}

// NewEndpoint creates an new miner endpoint.
func NewEndpoint(eCfg *EndpointConfig, listenAddr string) (*Endpoint, error) {
	endpoint := &Endpoint{
		listenAddr: listenAddr,
		cfg:        eCfg,
		clients:    make(map[string]*Client),
		connCh:     make(chan *connection, bufferSize),
		discCh:     make(chan struct{}, bufferSize),
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		desc := fmt.Sprintf("unable to create endpoint on %s", listenAddr)
		return nil, errs.PoolError(errs.Listener, desc)
	}
	endpoint.listener = listener
	return endpoint, nil
}

// removeClient removes a disconnected pool client from its associated endpoint.
func (e *Endpoint) removeClient(c *Client) {
	e.clientsMtx.Lock()
	delete(e.clients, c.extraNonce1)
	e.clientsMtx.Unlock()
	e.cfg.RemoveConnection(c.addr.IP.String())
}

// listen accepts incoming client connections on the endpoint.
// It must be run as a goroutine.
func (e *Endpoint) listen() {
	log.Infof("listening on %s", e.listenAddr)
	for {
		conn, err := e.listener.Accept()
		if err != nil {
			var opErr *net.OpError
			if errors.As(err, &opErr) {
				if opErr.Op == "accept" {
					if strings.Contains(opErr.Err.Error(),
						"use of closed network connection") {
						return
					}
				}
			}
			log.Errorf("unable to accept client connection: %v", err)
			return
		}
		e.connCh <- &connection{
			Conn: conn,
			Done: make(chan bool),
		}
	}
}

// connect creates new pool clients from established connections.
// It must be run as a goroutine.
func (e *Endpoint) connect(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			e.listener.Close()
			return

		case msg := <-e.connCh:
			addr := msg.Conn.RemoteAddr()
			tcpAddr, err := net.ResolveTCPAddr(addr.Network(), addr.String())
			if err != nil {
				log.Errorf("unable to parse tcp address: %v", err)
				continue
			}
			host := tcpAddr.IP.String()
			connCount := e.cfg.FetchHostConnections(host)
			if connCount >= e.cfg.MaxConnectionsPerHost {
				log.Errorf("exceeded maximum connections allowed per"+
					" host %d for %s", e.cfg.MaxConnectionsPerHost, host)
				msg.Conn.Close()
				close(msg.Done)
				continue
			}
			cCfg := &ClientConfig{
				ActiveNet:            e.cfg.ActiveNet,
				db:                   e.cfg.db,
				SoloPool:             e.cfg.SoloPool,
				Blake256Pad:          e.cfg.Blake256Pad,
				NonceIterations:      e.cfg.NonceIterations,
				FetchMinerDifficulty: e.cfg.FetchMinerDifficulty,
				Disconnect:           func() { e.wg.Done() },
				RemoveClient:         e.removeClient,
				SubmitWork:           e.cfg.SubmitWork,
				FetchCurrentWork:     e.cfg.FetchCurrentWork,
				WithinLimit:          e.cfg.WithinLimit,
				HashCalcThreshold:    hashCalcThreshold,
				MaxGenTime:           e.cfg.MaxGenTime,
				ClientTimeout:        e.cfg.ClientTimeout,
				SignalCache:          e.cfg.SignalCache,
				MonitorCycle:         e.cfg.MonitorCycle,
				MaxUpgradeTries:      e.cfg.MaxUpgradeTries,
				RollWorkCycle:        rollWorkCycle,
			}
			client, err := NewClient(ctx, msg.Conn, tcpAddr, cCfg)
			if err != nil {
				log.Errorf("unable to create client: %v", err)
				msg.Conn.Close()
				close(msg.Done)
				continue
			}
			e.clientsMtx.Lock()
			e.clients[client.extraNonce1] = client
			e.clientsMtx.Unlock()
			e.cfg.AddConnection(host)
			e.wg.Add(1)
			go client.run()

			log.Infof("Mining client connected. extranonce1=%s, addr=%s",
				client.extraNonce1, client.addr)

			close(msg.Done)
		}
	}
}

// disconnect relays client disconnections to the endpoint for processing.
// It must be run as a goroutine.
func (e *Endpoint) disconnect(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			e.clientsMtx.Lock()
			for _, client := range e.clients {
				client.cancel()
			}
			e.clientsMtx.Unlock()

			e.wg.Done()
			e.cfg.HubWg.Done()
			return

		case <-e.discCh:
			e.wg.Done()
		}
	}
}

// generateHashIDs generates hash ids of all client connections to the pool.
func (e *Endpoint) generateHashIDs() map[string]struct{} {
	e.clientsMtx.Lock()
	defer e.clientsMtx.Unlock()

	ids := make(map[string]struct{}, len(e.clients))
	for _, c := range e.clients {
		hashID := hashDataID(c.account, c.extraNonce1)
		ids[hashID] = struct{}{}
	}

	return ids
}

// run handles the lifecycle of all endpoint related processes.
// This should be run as a goroutine.
func (e *Endpoint) run(ctx context.Context) {
	e.wg.Add(1)
	go e.listen()
	go e.connect(ctx)
	go e.disconnect(ctx)
	e.wg.Wait()
}
