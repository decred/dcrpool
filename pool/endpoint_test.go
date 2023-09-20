// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"context"
	"errors"
	"math"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/v3"
)

func makeConn(listener *net.TCPListener, serverCh chan net.Conn) (net.Conn, net.Conn, error) {
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		return nil, nil, err
	}
	server := <-serverCh
	return conn, server, nil
}

func testEndpoint(t *testing.T) {
	const maxConnsPerHost = 3
	powLimit := chaincfg.SimNetParams().PowLimit
	iterations := math.Pow(2, float64(256-powLimit.BitLen()))
	maxGenTime := time.Second * 20
	blake256Pad := generateBlake256Pad()
	poolDiffs := NewDifficultySet(chaincfg.SimNetParams(),
		new(big.Rat).SetInt(powLimit), maxGenTime)
	connections := make(map[string]uint32)
	var connectionsMtx sync.RWMutex
	removeConn := make(chan struct{}, maxConnsPerHost)
	eCfg := &EndpointConfig{
		ActiveNet:             chaincfg.SimNetParams(),
		db:                    db,
		SoloPool:              true,
		Blake256Pad:           blake256Pad,
		NonceIterations:       iterations,
		MaxConnectionsPerHost: maxConnsPerHost,
		FetchMinerDifficulty: func(miner string) (*DifficultyInfo, error) {
			return poolDiffs.fetchMinerDifficulty(miner)
		},
		SubmitWork: func(_ context.Context, submission string) (bool, error) {
			return false, nil
		},
		FetchCurrentWork: func() string {
			return ""
		},
		WithinLimit: func(ip string, clientType int) bool {
			return true
		},
		AddConnection: func(host string) {
			connectionsMtx.Lock()
			connections[host]++
			connectionsMtx.Unlock()
		},
		RemoveConnection: func(host string) {
			connectionsMtx.Lock()
			connections[host]--
			connectionsMtx.Unlock()
			removeConn <- struct{}{}
		},
		FetchHostConnections: func(host string) uint32 {
			connectionsMtx.RLock()
			defer connectionsMtx.RUnlock()
			return connections[host]
		},
		SignalCache: func(_ CacheUpdateEvent) {
			// Do nothing.
		},
		MonitorCycle:    time.Minute,
		MaxUpgradeTries: 5,
		ClientTimeout:   time.Second * 30,
	}
	endpoint, err := NewEndpoint(eCfg, "0.0.0.0:3030")
	if err != nil {
		t.Fatalf("[NewEndpoint] unexpected error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		endpoint.run(ctx)
		wg.Done()
	}()
	sendToConnChanOrFatal := func(msg *connection) {
		select {
		case endpoint.connCh <- msg:
		case <-ctx.Done():
			t.Fatalf("unexpected endpoint shutdown")
		}
		select {
		case <-msg.Done:
		case <-ctx.Done():
			t.Fatalf("unexpected endpoint shutdown")
		}
	}

	laddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:3031")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ln, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer ln.Close()
	serverCh := make(chan net.Conn)
	go func() {
		for {
			conn, err := ln.Accept()
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
				log.Errorf("unable to accept connection %v", err)
				return
			}
			serverCh <- conn
		}
	}()

	// Send the endpoint a client-side connection.
	connA, srvA, err := makeConn(ln, serverCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer connA.Close()
	defer srvA.Close()
	msgA := &connection{
		Conn: connA,
		Done: make(chan struct{}),
	}
	sendToConnChanOrFatal(msgA)
	addr := connA.RemoteAddr()
	tcpAddr, err := net.ResolveTCPAddr(addr.Network(), addr.String())
	if err != nil {
		log.Errorf("unable to parse tcp address: %v", err)
	}

	// Ensure the host connection count for the client got incremented.
	host := tcpAddr.IP.String()
	hostConnections := endpoint.cfg.FetchHostConnections(host)
	if hostConnections != 1 {
		t.Fatalf("[FetchHostConnections] expected %d connection(s) "+
			"for host %s, got %d", 1, host, hostConnections)
	}

	// Add two more clients.
	connB, srvB, err := makeConn(ln, serverCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer connB.Close()
	defer srvB.Close()
	msgB := &connection{
		Conn: connB,
		Done: make(chan struct{}),
	}
	sendToConnChanOrFatal(msgB)
	connC, srvC, err := makeConn(ln, serverCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer connC.Close()
	defer srvC.Close()
	msgC := &connection{
		Conn: connC,
		Done: make(chan struct{}),
	}
	sendToConnChanOrFatal(msgC)

	// Ensure the connected clients to the host got incremented to 3.
	hostConnections = endpoint.cfg.FetchHostConnections(host)
	if hostConnections != 3 {
		t.Fatalf("[FetchHostConnections] expected %d connection(s) "+
			"for host %s, got %d", 3, host, hostConnections)
	}

	// Add another client.
	connD, srvD, err := makeConn(ln, serverCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer connD.Close()
	defer srvD.Close()
	msgD := &connection{
		Conn: connD,
		Done: make(chan struct{}),
	}
	sendToConnChanOrFatal(msgD)

	// Ensure the connected clients count to the host stayed at 3 because
	// the recent connection got rejected due to MaxConnectionCountPerHost
	// being reached.
	hostConnections = endpoint.cfg.FetchHostConnections(host)
	if hostConnections != 3 {
		t.Fatalf("[FetchHostConnections] expected %d connection(s) "+
			"for host %s, got %d", 3, host, hostConnections)
	}

	// Remove all clients and wait for their removal.
	endpoint.clientsMtx.Lock()
	clients := make([]*Client, 0, len(endpoint.clients))
	for _, cl := range endpoint.clients {
		clients = append(clients, cl)
	}
	endpoint.clientsMtx.Unlock()
	for _, cl := range clients {
		cl.shutdown()
	}
	for i := 0; i < len(clients); i++ {
		select {
		case <-removeConn:
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for connection removal")
		}
	}

	// Ensure there are no connected clients to the host.
	hostConnections = endpoint.cfg.FetchHostConnections(host)
	if hostConnections != 0 {
		t.Fatalf("[FetchHostConnections] expected %d connection(s) for host %s"+
			" connections, got %d", 0, host, hostConnections)
	}

	// Ensure the endpoint listener can create connections.
	ep, err := net.ResolveTCPAddr("tcp", "127.0.0.1:3030")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	conn, err := net.Dial("tcp", ep.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer conn.Close()

	cancel()
	wg.Wait()
}
