package pool

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"math/big"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
)

func testHub(t *testing.T, db *bolt.DB) {
	minPayment, err := dcrutil.NewAmount(2.0)
	if err != nil {
		t.Fatalf("[NewAmount] unexpected error: %v", err)
	}
	maxTxFeeReserve, err := dcrutil.NewAmount(0.1)
	if err != nil {
		t.Fatalf("[NewAmount] unexpected error: %v", err)
	}
	activeNet := chaincfg.SimNetParams()
	powLimit := chaincfg.SimNetParams().PowLimit
	powLimitF, _ := new(big.Float).SetInt(powLimit).Float64()
	iterations := math.Pow(2, 256-math.Floor(math.Log2(powLimitF)))
	hcfg := &HubConfig{
		ActiveNet:             activeNet,
		DB:                    db,
		PoolFee:               0.1,
		LastNPeriod:           120,
		SoloPool:              false,
		PaymentMethod:         PPS,
		MinPayment:            minPayment,
		MaxGenTime:            20,
		PoolFeeAddrs:          []dcrutil.Address{poolFeeAddrs},
		MaxTxFeeReserve:       maxTxFeeReserve,
		MaxConnectionsPerHost: 2,
		NonceIterations:       iterations,
		MinerPorts: map[string]uint32{
			CPU:           5050,
			InnosiliconD9: 5052,
			AntminerDR3:   5553,
			AntminerDR5:   5554,
			WhatsminerD1:  5555,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	hub, err := NewHub(cancel, hcfg)
	if err != nil {
		t.Fatalf("[NewHub] uexpected error: %v", err)
	}

	err = hub.Listen()
	if err != nil {
		t.Fatalf("[Listen] uexpected error: %v", err)
	}
	go hub.Run(ctx)

	// Ensure account X exists.
	if !hub.AccountExists(xID) {
		t.Fatalf("expected account with id %s to exist", xID)
	}

	// Ensure the gui CSRF secret can be generated.
	csrf, err := hub.CSRFSecret()
	if err != nil {
		t.Fatalf("[CSRFSecret] unexpected error: %v", err)
	}
	if csrf == nil {
		t.Fatal("expected a non-nil csrf secref")
	}

	// Ensure the database can be backed up.
	rr := httptest.NewRecorder()
	err = hub.BackupDB(rr)
	if err != nil {
		t.Fatalf("[BackupDB] unexpected error: %v", err)
	}
	body, err := ioutil.ReadAll(rr.Result().Body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("expected a response body with data")
	}

	// Ensure work quotas are generated as expected.
	now := time.Now()
	minimumTime := now.Add(-(time.Second * 60)).UnixNano()
	xWeight := new(big.Rat).SetFloat64(1.0)
	yWeight := new(big.Rat).SetFloat64(4.0)
	err = persistShare(db, xID, xWeight, minimumTime)
	if err != nil {
		t.Fatal(err)
	}
	err = persistShare(db, yID, yWeight, now.UnixNano())
	if err != nil {
		t.Fatal(err)
	}

	quotas, err := hub.FetchWorkQuotas()
	if err != nil {
		t.Fatalf("[FetchWorkQuotas] unexpected error: %v", err)
	}
	if len(quotas) != 2 {
		t.Fatalf("expected a work quota length of 2, got %v", len(quotas))
	}

	var xQuota, yQuota *Quota
	for _, q := range quotas {
		if q.AccountID == xID {
			xQuota = q
		}
		if q.AccountID == yID {
			yQuota = q
		}
	}

	sum := new(big.Rat).Add(yQuota.Percentage, xQuota.Percentage)
	if sum.Cmp(new(big.Rat).SetInt(new(big.Int).SetInt64(1))) != 0 {
		t.Fatalf("expected the sum of share percentages to be 1, got %v", sum)
	}

	port := uint32(3031)
	laddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", "127.0.0.1", port))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ln, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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

	// Ensure the hub keeps track of connected clients and their statistics.
	var cpuEndpoint *Endpoint
	for _, e := range hub.endpoints {
		if e.miner == CPU {
			cpuEndpoint = e
			break
		}
	}

	// Send the cpu endpoint a client-side connection.
	connA, _, err := makeConn(ln, serverCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cpuEndpoint.connCh <- connA
	time.Sleep(time.Millisecond * 100)
	if !hub.HasClients() {
		t.Fatal("expected hub to have clients")
	}

	addr := connA.RemoteAddr()
	tcpAddr, err := net.ResolveTCPAddr(addr.Network(), addr.String())
	if err != nil {
		t.Fatalf("unable to parse tcp address: %v", err)
	}

	host := tcpAddr.IP.String()
	connections := hub.fetchHostConnections(host)
	if connections != 1 {
		t.Fatalf("expected 1 client connection from host %s, got %v",
			host, connections)
	}
	cInfo := hub.FetchClientInfo()
	if len(cInfo) != 1 {
		t.Fatalf("[FetchClientInfo] expected a client info size "+
			"of 1, got %d", len(cInfo))
	}

	// Ensure there are no connected clients for test accounts
	aInfo := hub.FetchAccountClientInfo(xID)
	if len(aInfo) != 0 {
		t.Fatalf("[FetchClientInfo] expected a client info size of 0"+
			" for account x, got %d", len(cInfo))
	}

	// Ensure connected clients do not have an associated account.
	aInfo = hub.FetchAccountClientInfo("")
	if len(aInfo) != 1 {
		t.Fatalf("[FetchClientInfo] expected a client info size "+
			"of 1 for clients with no associated account, got %d", len(cInfo))
	}

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	cancel()
	hub.wg.Wait()
}
