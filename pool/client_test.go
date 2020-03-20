package pool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/v2"
	bolt "go.etcd.io/bbolt"
)

func testClient(t *testing.T, db *bolt.DB) {
	port := uint32(3030)
	laddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", "127.0.0.1", port))
	if err != nil {
		t.Fatalf("[ResolveTCPAddr] unexpected error: %v", err)
	}

	ln, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		t.Fatalf("[ListenTCP] unexpected error: %v", err)
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

	// Create a new client.
	c, s, err := makeConn(ln, serverCh)
	if err != nil {
		t.Fatalf("[makeConn] unexpected error: %v", err)
	}

	addr := c.RemoteAddr()
	tcpAddr, err := net.ResolveTCPAddr(addr.Network(), addr.String())
	if err != nil {
		t.Fatalf("unable to parse tcp addresss: %v", err)
	}

	miner := CPU
	var minerMtx sync.RWMutex
	setMiner := func(m string) {
		minerMtx.Lock()
		miner = m
		minerMtx.Unlock()
	}
	powLimit := chaincfg.SimNetParams().PowLimit
	powLimitF, _ := new(big.Float).SetInt(powLimit).Float64()
	iterations := math.Pow(2, 256-math.Floor(math.Log2(powLimitF)))
	blake256Pad := generateBlake256Pad()
	maxGenTime := time.Second * 20
	poolDiffs, err := NewDifficultySet(chaincfg.SimNetParams(),
		new(big.Rat).SetInt(powLimit), maxGenTime)
	if err != nil {
		t.Fatalf("[NewPoolDifficulty] unexpected error: %v", err)
	}
	diffInfo, err := poolDiffs.fetchMinerDifficulty(miner)
	if err != nil {
		t.Fatalf("[fetchMinerDifficulty] unexpected error: %v", err)
	}

	var currentWork string
	var currentWorkMtx sync.RWMutex
	setCurrentWork := func(work string) {
		currentWorkMtx.Lock()
		currentWork = work
		currentWorkMtx.Unlock()
	}
	cCfg := &ClientConfig{
		ActiveNet:       chaincfg.SimNetParams(),
		DB:              db,
		Blake256Pad:     blake256Pad,
		NonceIterations: iterations,
		FetchMiner: func() string {
			minerMtx.RLock()
			defer minerMtx.RUnlock()
			return miner
		},
		SoloPool:       false,
		DifficultyInfo: diffInfo,
		EndpointWg:     new(sync.WaitGroup),
		RemoveClient:   func(c *Client) {},
		SubmitWork: func(submission *string) (bool, error) {
			return false, nil
		},
		FetchCurrentWork: func() string {
			currentWorkMtx.RLock()
			defer currentWorkMtx.RUnlock()
			return currentWork
		},
		WithinLimit: func(ip string, clientType int) bool {
			return true
		},
		HashCalcThreshold: 1,
	}
	client, err := NewClient(c, tcpAddr, cCfg)
	if err != nil {
		t.Fatalf("[NewClient] unexpected error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	go client.run(ctx)
	time.Sleep(time.Millisecond * 50)

	sE := json.NewEncoder(s)
	sR := bufio.NewReaderSize(s, MaxMessageSize)

	recvCh := make(chan []byte)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			data, err := sR.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					cancel()
					return
				}

				nErr, ok := err.(*net.OpError)
				if !ok {
					log.Errorf("failed to read bytes: %v", err)
					cancel()
					return
				}

				if nErr != nil {
					if nErr.Op == "read" && nErr.Net == "tcp" {
						switch {
						case nErr.Timeout():
							log.Errorf("read timeout: %v", err)
						case !nErr.Timeout():
							log.Errorf("read error: %v", err)
						}
						cancel()
						return
					}
				}

				log.Errorf("failed to read bytes: %v %T", err, err)
				cancel()
				return
			}

			recvCh <- data
		}
	}()

	// Send an authorize request.
	id := uint64(1)
	r := AuthorizeRequest(&id, "mn", "SsiuwSRYvH7pqWmRxFJWR8Vmqc3AWsjmK2Y")
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}

	// Ensure an authorize response was sent back.
	data := <-recvCh
	msg, mType, err := IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}

	if mType != ResponseMessage {
		t.Fatalf("expected an auth response message, got %v", mType)
	}

	resp, ok := msg.(*Response)
	if !ok {
		t.Fatalf("expected response with id %d, got %d", *r.ID, resp.ID)
	}

	if resp.ID != *r.ID {
		t.Fatalf("expected response with id %d, got %d", *r.ID, resp.ID)
	}

	// Ensure a difficulty notification was sent
	data = <-recvCh
	msg, mType, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}

	if mType != NotificationMessage {
		t.Fatalf("expected a notification message, got %v", mType)
	}

	req, ok := msg.(*Request)
	if !ok {
		t.Fatalf("unable to cast message as request")
	}

	if req.Method != SetDifficulty {
		t.Fatalf("expected %s message method, got %s", SetDifficulty, req.Method)
	}

	// Send a subscribe request.
	setMiner(WhatsminerD1)
	id++
	r = SubscribeRequest(&id, "mcpu", "1.0.1", "mn001")
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}

	// Ensure an subscribe response was sent back.
	data = <-recvCh
	_, mType, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}

	if mType != ResponseMessage {
		t.Fatalf("expected a subscribe response message, got %v", mType)
	}

	setMiner(AntminerDR3)
	id++
	r.ID = &id
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}

	// Ensure an subscribe response was sent back.
	data = <-recvCh
	_, mType, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}

	if mType != ResponseMessage {
		t.Fatalf("expected a subscribe response message, got %v", mType)
	}

	// Ensure an subscribe response was sent back.
	setMiner(CPU)
	id++
	r.ID = &id
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}

	data = <-recvCh
	msg, mType, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}

	if mType != ResponseMessage {
		t.Fatalf("expected a subscribe response message, got %v", mType)
	}

	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("expected subsriberesponse with id %d, got %d", *r.ID, resp.ID)
	}

	if resp.ID != *r.ID {
		t.Fatalf("expected suscribe response with id %d, got %d", *r.ID, resp.ID)
	}

	// Ensure the client is authorized and subscribed for work updates.
	client.authorizedMtx.Lock()
	authorized := client.authorized
	client.authorizedMtx.Unlock()

	if !authorized {
		t.Fatalf("expected an authorized mining client")
	}

	client.subscribedMtx.Lock()
	subscribed := client.subscribed
	client.subscribedMtx.Unlock()

	if !subscribed {
		t.Fatalf("expected a subscribed mining client")
	}

	workE := "07000000022b580ca96146e9c85fa1ee2ec02e0e2579a" +
		"f4e3881fc619ec52d64d83e0000bd646e312ff574bc90e08ed91f1" +
		"d99a85b318cb4464f2a24f9ad2bf3b9881c2bc9c344adde75e89b1" +
		"4b627acce606e6d652915bdb71dcf5351e8ad6128faab9e0100000" +
		"00000000000000000000000003e133920204e00000000000029000" +
		"000a6030000954cee5d00000000000000000000000000000000000" +
		"000000000000000000000000000000000000000000000800000010" +
		"0000000000005a0"

	job, err := NewJob(workE, 41)
	if err != nil {
		t.Fatalf("unable to create job %v", err)
	}

	err = job.Create(client.cfg.DB)
	if err != nil {
		t.Fatalf("failed to persist job %v", err)
	}

	blockVersion := workE[:8]
	prevBlock := workE[8:72]
	genTx1 := workE[72:288]
	nBits := workE[232:240]
	nTime := workE[272:280]
	genTx2 := workE[352:360]

	// Send a work notification.
	r = WorkNotification(job.UUID, prevBlock, genTx1, genTx2,
		blockVersion, nBits, nTime, true)

	client.ch <- r

	// Ensure cpu work notification was received.
	cpuWork := <-recvCh
	msg, mType, err = IdentifyMessage(cpuWork)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}

	if mType != NotificationMessage {
		t.Fatalf("expected a notification message, got %v", mType)
	}

	req, ok = msg.(*Request)
	if !ok {
		t.Fatalf("unable to cast message as request")
	}

	if req.Method != Notify {
		t.Fatalf("expected %s message method, got %s", Notify, req.Method)
	}

	// Update the miner type of the endpoint.
	setMiner(InnosiliconD9)

	// Send another work notification.
	client.ch <- r

	// Ensure the work notification recieved is different from the cpu work
	// received.
	d9Work := <-recvCh
	if bytes.Equal(cpuWork, d9Work) {
		t.Fatalf("expected innosilicond9 work to be different from cpu work")
	}

	// Update the miner type of the endpoint.
	setMiner(WhatsminerD1)

	// Send another work notification.
	client.ch <- r

	// Ensure the work notification recieved is different from the D9 work
	// received.
	d1Work := <-recvCh
	if bytes.Equal(d1Work, d9Work) {
		t.Fatalf("expected whatsminer d1 work to be different from innosilion d9 work")
	}

	// Update the miner type of the endpoint.
	setMiner(AntminerDR3)

	// Send another work notification.
	client.ch <- r

	// Ensure the work notification recieved is different from the D1 work
	// received.
	dr3Work := <-recvCh
	if bytes.Equal(d1Work, dr3Work) {
		t.Fatalf("expected antminer dr3 work to be different from whatsminer d1 work")
	}

	// Update the miner type of the endpoint.
	setMiner(AntminerDR5)

	// Send another work notification.
	client.ch <- r

	// Ensure the work notification recieved is different from the D1 work
	// received.
	dr5Work := <-recvCh
	if !bytes.Equal(dr5Work, dr3Work) {
		t.Fatalf("expected antminer dr3 work to be equal to antminer dr5 work")
	}

	// Update the miner type of the endpoint.
	setMiner(CPU)

	id = uint64(3)
	sub := SubmitWorkRequest(&id, "tcl", job.UUID, "00000000",
		"954cee5d", "6ddf0200")

	// Send a work submission.
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}

	// Ensure a response was sent back for the cpu submission.
	cpuSub := <-recvCh

	msg, mType, err = IdentifyMessage(cpuSub)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}

	if mType != ResponseMessage {
		t.Fatalf("expected a response message, got %v", mType)
	}

	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("unable to cast message as response")
	}

	if resp.ID != *sub.ID {
		t.Fatalf("expected a response with id %d, got %d", *sub.ID, resp.ID)
	}

	// Update the miner type of the endpoint.
	setMiner(WhatsminerD1)

	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000",
		"954cee5d", "6ddf0200")

	// Send a work submission.
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}

	// Ensure a response was sent back for the whatsminer submission.
	d1Sub := <-recvCh
	msg, mType, err = IdentifyMessage(d1Sub)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}
	if mType != ResponseMessage {
		t.Fatalf("expected a response message, got %v", mType)
	}
	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("unable to cast message as response")
	}
	if resp.ID != *sub.ID {
		t.Fatalf("expected a response with id %d, got %d", *sub.ID, resp.ID)
	}

	// Update the miner type of the endpoint.
	setMiner(AntminerDR3)

	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000",
		"954cee5d", "6ddf0200")

	// Send a work submission.
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}

	// Ensure a response was sent back for the antminer dr3 submission.
	dr3Sub := <-recvCh
	msg, mType, err = IdentifyMessage(dr3Sub)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}

	if mType != ResponseMessage {
		t.Fatalf("expected a response message, got %v", mType)
	}

	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("unable to cast message as response")
	}

	if resp.ID != *sub.ID {
		t.Fatalf("expected a response with id %d, got %d", *sub.ID, resp.ID)
	}

	// Update the miner type of the endpoint.
	setMiner(AntminerDR5)

	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000",
		"954cee5d", "6ddf0200")

	// Send a work submission.
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}

	// Ensure a response was sent back for the antminer dr5 submission.
	dr5Sub := <-recvCh
	msg, mType, err = IdentifyMessage(dr5Sub)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}
	if mType != ResponseMessage {
		t.Fatalf("expected a response message, got %v", mType)
	}
	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("unable to cast message as response")
	}
	if resp.ID != *sub.ID {
		t.Fatalf("expected a response with id %d, got %d", *sub.ID, resp.ID)
	}

	// Update the miner type of the endpoint.
	setMiner(InnosiliconD9)

	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000",
		"954cee5d", "6ddf0200")

	// Send a work submission.
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}

	// Trigger time-rolled work updates by setting the hub's current work.
	setCurrentWork(workE)

	// Ensure a response was sent back for the antminer d9 submission.
	d9Sub := <-recvCh

	msg, mType, err = IdentifyMessage(d9Sub)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}

	if mType != ResponseMessage {
		t.Fatalf("expected a response message, got %v", mType)
	}

	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("unable to cast message as response")
	}

	if resp.ID != *sub.ID {
		t.Fatalf("expected a response with id %d, got %d", *sub.ID, resp.ID)
	}

	// Ensure the client receives time-rolled work.
	timeRolledWork := <-recvCh
	msg, mType, err = IdentifyMessage(timeRolledWork)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}

	if mType != NotificationMessage {
		t.Fatalf("expected a notification message, got %v", mType)
	}

	req, ok = msg.(*Request)
	if !ok {
		t.Fatalf("unable to cast message as request")
	}

	if req.Method != Notify {
		t.Fatalf("expected %s message method, got %s", Notify, req.Method)
	}

	// Fake a bunch of submissions and calculate the hash rate.
	setMiner(CPU)
	atomic.StoreInt64(&client.submissions, 50)
	time.Sleep(time.Second * 2)
	hash := client.fetchHashRate()
	if hash == ZeroRat {
		t.Fatal("expected a non-nil client hash rate")
	}

	// Empty the job bucket.
	err = emptyBucket(db, jobBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	cancel()
	client.cfg.EndpointWg.Wait()
}
