package pool

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg/v2"
)

func makeConn(listener *net.TCPListener, serverCh chan net.Conn) (net.Conn, net.Conn, error) {
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		return nil, nil, err
	}
	server := <-serverCh
	return conn, server, nil
}

func testEndpoint(t *testing.T, db *bolt.DB) {
	miner := CPU
	powLimit := chaincfg.SimNetParams().PowLimit
	powLimitF, _ := new(big.Float).SetInt(powLimit).Float64()
	iterations := math.Pow(2, 256-math.Floor(math.Log2(powLimitF)))
	maxGenTime := new(big.Int).SetUint64(20)
	blake256Pad := generateBlake256Pad()
	poolDiffs, err := NewPoolDifficulty(chaincfg.SimNetParams(),
		new(big.Rat).SetInt(powLimit), maxGenTime)
	if err != nil {
		t.Fatalf("[NewPoolDifficulty] unexpected error: %v", err)
	}
	diffInfo, err := poolDiffs.fetchMinerDifficulty(CPU)
	if err != nil {
		t.Fatalf("[fetchMinerDifficulty] unexpected error: %v", err)
	}

	connections := make(map[string]uint32)
	var connectionsMtx sync.RWMutex
	eCfg := &EndpointConfig{
		ActiveNet:             chaincfg.SimNetParams(),
		DB:                    db,
		SoloPool:              true,
		Blake256Pad:           blake256Pad,
		NonceIterations:       iterations,
		MaxConnectionsPerHost: 3,
		HubWg:                 new(sync.WaitGroup),
		SubmitWork: func(submission *string) (bool, error) {
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
		},
		FetchHostConnections: func(host string) uint32 {
			connectionsMtx.RLock()
			defer connectionsMtx.RUnlock()
			return connections[host]
		},
	}
	port := uint32(3030)
	endpoint, err := NewEndpoint(eCfg, diffInfo, port, miner)
	if err != nil {
		t.Fatalf("[NewEndpoint] unexpected error: %v", err)
	}
	endpoint.cfg.HubWg.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	go endpoint.run(ctx)
	time.Sleep(time.Millisecond * 100)
	laddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", "127.0.0.1", port+1))
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
				if opErr, ok := err.(*net.OpError); ok {
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
	connA, _, err := makeConn(ln, serverCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer connA.Close()
	endpoint.connCh <- connA
	time.Sleep(time.Millisecond * 100)
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
	connB, _, err := makeConn(ln, serverCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	endpoint.connCh <- connB
	time.Sleep(time.Millisecond * 100)
	connC, _, err := makeConn(ln, serverCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	endpoint.connCh <- connC
	time.Sleep(time.Millisecond * 100)

	// Ensure the connected clients to the host got incremented to 3.
	hostConnections = endpoint.cfg.FetchHostConnections(host)
	if hostConnections != 3 {
		t.Fatalf("[FetchHostConnections] expected %d connection(s) "+
			"for host %s, got %d", 3, host, hostConnections)
	}

	// Add another client.
	connD, _, err := makeConn(ln, serverCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	endpoint.connCh <- connD
	time.Sleep(time.Millisecond * 100)

	// Ensure the connected clients count to the host stayed at 3 because
	// the recent connection got rejected due to MaxConnectionCountPerHost
	// being reached.
	hostConnections = endpoint.cfg.FetchHostConnections(host)
	if hostConnections != 3 {
		t.Fatalf("[FetchHostConnections] expected %d connection(s) "+
			"for host %s, got %d", 3, host, hostConnections)
	}

	// Remove all clients.
	endpoint.clientsMtx.Lock()
	clients := make([]*Client, len(endpoint.clients))
	i := 0
	for _, cl := range endpoint.clients {
		clients[i] = cl
		i++
	}
	endpoint.clientsMtx.Unlock()
	for _, cl := range clients {
		cl.shutdown()
	}

	// Ensure there are no connected clients to the host.
	hostConnections = endpoint.cfg.FetchHostConnections(host)
	if hostConnections != 0 {
		t.Fatalf("[FetchHostConnections] expected %d connection(s) for host %s"+
			" connections, got %d", 0, host, hostConnections)
	}
	cancel()
	endpoint.cfg.HubWg.Wait()
}
