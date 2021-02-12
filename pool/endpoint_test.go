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
	powLimit := chaincfg.SimNetParams().PowLimit
	powLimitF, _ := new(big.Float).SetInt(powLimit).Float64()
	iterations := math.Pow(2, 256-math.Floor(math.Log2(powLimitF)))
	maxGenTime := time.Second * 20
	blake256Pad := generateBlake256Pad()
	poolDiffs := NewDifficultySet(chaincfg.SimNetParams(),
		new(big.Rat).SetInt(powLimit), maxGenTime)
	connections := make(map[string]uint32)
	var connectionsMtx sync.RWMutex
	eCfg := &EndpointConfig{
		ActiveNet:             chaincfg.SimNetParams(),
		db:                    db,
		SoloPool:              true,
		Blake256Pad:           blake256Pad,
		NonceIterations:       iterations,
		MaxConnectionsPerHost: 3,
		HubWg:                 new(sync.WaitGroup),
		FetchMinerDifficulty: func(miner string) (*DifficultyInfo, error) {
			return poolDiffs.fetchMinerDifficulty(miner)
		},
		SubmitWork: func(_ context.Context, submission *string) (bool, error) {
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
	endpoint.cfg.HubWg.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	endpoint.wg.Add(1)
	go endpoint.run(ctx)
	time.Sleep(time.Millisecond * 100)

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
		Done: make(chan bool),
	}
	endpoint.connCh <- msgA
	<-msgA.Done
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
		Done: make(chan bool),
	}
	endpoint.connCh <- msgB
	<-msgB.Done
	connC, srvC, err := makeConn(ln, serverCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer connC.Close()
	defer srvC.Close()
	msgC := &connection{
		Conn: connC,
		Done: make(chan bool),
	}
	endpoint.connCh <- msgC
	<-msgC.Done

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
		Done: make(chan bool),
	}
	endpoint.connCh <- msgD
	<-msgD.Done

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
	clients := make([]*Client, 0, len(endpoint.clients))
	for _, cl := range endpoint.clients {
		clients = append(clients, cl)
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
	endpoint.cfg.HubWg.Wait()
}
