package pool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	"github.com/decred/dcrd/chaincfg/v3"
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

	// Create a new client connection.
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
		SubmitWork: func(_ context.Context, submission *string) (bool, error) {
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
		ClientTimeout:     time.Millisecond * 1300,
		SignalCache: func(_ CacheUpdateEvent) {
			// Do nothing.
		},
	}
	client, err := NewClient(c, tcpAddr, cCfg)
	if err != nil {
		t.Fatalf("[NewClient] unexpected error: %v", err)
	}
	go client.run(client.ctx)
	time.Sleep(time.Millisecond * 50)
	sE := json.NewEncoder(s)
	sR := bufio.NewReaderSize(s, maxMessageSize)

	recvCh := make(chan []byte)
	readMsg := func(c *Client, r *bufio.Reader) {
		for {
			select {
			case <-c.ctx.Done():
				return
			default:
			}

			data, err := r.ReadBytes('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					c.cancel()
					return
				}
				var nErr *net.OpError
				if !errors.As(err, &nErr) {
					log.Errorf("failed to read bytes: %v", err)
					c.cancel()
					return
				}
				if nErr.Op == "read" && nErr.Net == "tcp" {
					switch {
					case nErr.Timeout():
						log.Errorf("read timeout: %v", err)
					case !nErr.Timeout():
						log.Errorf("read error: %v", err)
					}
					c.cancel()
					return
				}

				log.Errorf("failed to read bytes: %v %T", err, err)
				c.cancel()
				return
			}
			recvCh <- data
		}
	}

	go readMsg(client, sR)

	// Ensure the client receives an error response when a malformed
	// authorize request is sent.
	id := uint64(1)
	r := &Request{
		ID:     &id,
		Method: Authorize,
		Params: []string{},
	}
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	var msg Message
	var mType int
	var data []byte
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case data = <-recvCh:
	}
	msg, mType, err = IdentifyMessage(data)
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
	if resp.Error == nil {
		t.Fatal("expected a malformed authorize error response")
	}

	// Ensure a CPU client receives an error response when a malformed
	// authorize request with an invalid user format is sent.
	id++
	r = &Request{
		ID:     &id,
		Method: Authorize,
		Params: []string{"mn", ""},
	}
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case data = <-recvCh:
	}
	msg, _, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}
	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("expected response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.Error == nil {
		t.Fatal("expected an invalid username authorize error response")
	}

	// Ensure a CPU client receives an error response when it has
	// exhausted its request limits.
	client.cfg.WithinLimit = func(ip string, clientType int) bool {
		return false
	}
	id++
	r = AuthorizeRequest(&id, "mn", "SsiuwSRYvH7pqWmRxFJWR8Vmqc3AWsjmK2Y")
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case data = <-recvCh:
	}
	msg, _, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}
	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("expected response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.Error == nil {
		t.Fatal("expected a rate limit error response")
	}
	client.cfg.WithinLimit = func(ip string, clientType int) bool {
		return true
	}

	// Ensure a CPU client receives a valid non-error response when
	// a valid authorize request is sent.
	id++
	r = AuthorizeRequest(&id, "mn", "SsiuwSRYvH7pqWmRxFJWR8Vmqc3AWsjmK2Y")
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case data = <-recvCh:
	}
	msg, mType, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}
	if mType != ResponseMessage {
		t.Fatalf("expected an auth response message (%d), got %d",
			mType, msg.MessageType())
	}
	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("expected response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.ID != *r.ID {
		t.Fatalf("expected response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("expected non-error authorize response, got %v", resp.Error)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case data = <-recvCh:
	}
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

	// Ensure a Whatsminer D1 client receives an error response when a
	// malformed subscribe request with an invalid user format is sent.
	setMiner(WhatsminerD1)
	id++
	r = &Request{
		ID:     &id,
		Method: Subscribe,
		Params: nil,
	}
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case data = <-recvCh:
	}
	msg, mType, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}
	if mType != ResponseMessage {
		t.Fatalf("expected a subscribe response message, got %v", mType)
	}
	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("expected response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.ID != *r.ID {
		t.Fatalf("expected response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.Error == nil {
		t.Fatal("expected a malformed subscribe request error response")
	}

	// Ensure a Whatsminer D1 client receives an error response when
	// it has exhausted its request limits.
	client.cfg.WithinLimit = func(ip string, clientType int) bool {
		return false
	}
	id++
	r = SubscribeRequest(&id, "mcpu", "1.0.1", "mn001")
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case data = <-recvCh:
	}
	msg, mType, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}
	if mType != ResponseMessage {
		t.Fatalf("expected a subscribe response message, got %v", mType)
	}
	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("expected response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.ID != *r.ID {
		t.Fatalf("expected response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.Error == nil {
		t.Fatal("expected a rate limit error response")
	}
	client.cfg.WithinLimit = func(ip string, clientType int) bool {
		return true
	}

	// Ensure a Whatsminer D1 client receives a valid non-error
	// response when a valid subscribe request is sent.
	id++
	r = SubscribeRequest(&id, "mcpu", "1.0.1", "")
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case data = <-recvCh:
	}
	_, mType, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}
	if mType != ResponseMessage {
		t.Fatalf("expected a subscribe response message, got %v", mType)
	}

	// Ensure an Antminer DR3 client receives a valid non-error
	// response when a valid subscribe request is sent.
	setMiner(AntminerDR3)
	id++
	r.ID = &id
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case data = <-recvCh:
	}
	_, mType, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}
	if mType != ResponseMessage {
		t.Fatalf("expected a subscribe response message, got %v", mType)
	}

	// Ensure an Obelisk DCR1 client receives a valid non-error
	// response when a valid subscribe request is sent.
	setMiner(ObeliskDCR1)
	id++
	r.ID = &id
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case data = <-recvCh:
	}
	msg, mType, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}
	if mType != ResponseMessage {
		t.Fatalf("expected a subscribe response message, got %v", mType)
	}
	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("expected subsribe response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.ID != *r.ID {
		t.Fatalf("expected suscribe response with id %d, got %d", *r.ID, resp.ID)
	}

	// Ensure a CPU client receives a valid non-error response when a
	// valid subscribe request is sent.
	setMiner(CPU)
	id++
	r.ID = &id
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case data = <-recvCh:
	}
	msg, mType, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}
	if mType != ResponseMessage {
		t.Fatalf("expected a subscribe response message, got %v", mType)
	}
	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("expected subsribe response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.ID != *r.ID {
		t.Fatalf("expected suscribe response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("expected non-error subscribe response, got %v", resp.Error)
	}

	// Ensure the CPU client is now authorized and subscribed
	// for work updates.
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

	// Send a work notification to the CPU client.
	r = WorkNotification(job.UUID, prevBlock, genTx1, genTx2,
		blockVersion, nBits, nTime, true)
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case client.ch <- r:
	}

	var cpuWork []byte
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case cpuWork = <-recvCh:
	}
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

	// Claim a weighted share for the CPU client.
	err = client.claimWeightedShare()
	if err != nil {
		t.Fatalf("[claimWeightedShare (CPU)] unexpected error: %v", err)
	}

	// Ensure a CPU client receives an error response when
	// it triggers a weighted share error.
	client.cfg.SoloPool = true
	err = client.claimWeightedShare()
	if err == nil {
		t.Fatalf("[claimWeightedShare (CPU)] expected a solo pool mode error")
	}
	client.cfg.SoloPool = false
	client.cfg.ActiveNet = chaincfg.MainNetParams()
	err = client.claimWeightedShare()
	if err == nil {
		t.Fatalf("[claimWeightedShare (CPU)] expected an active " +
			"network cpu share error")
	}
	client.cfg.ActiveNet = chaincfg.SimNetParams()

	// Ensure the last work time of the CPU client was updated
	// on receiving work.
	lastWorkTime := atomic.LoadInt64(&client.lastWorkTime)
	if lastWorkTime == 0 {
		t.Fatalf("expected last work time for %s connection "+
			"to be more than zero, got %d", client.id,
			client.lastWorkTime)
	}

	// Send a work notification to an Innosilicon D9 client.
	setMiner(InnosiliconD9)
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case client.ch <- r:
	}

	// Ensure the work notification received is unique to the D9.
	var d9Work []byte
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case d9Work = <-recvCh:
	}
	if bytes.Equal(cpuWork, d9Work) {
		t.Fatalf("expected innosilicond9 work to be different from cpu work")
	}

	// Claim a weighted share for the Innosilicon D9 client.
	err = client.claimWeightedShare()
	if err != nil {
		t.Fatalf("[claimWeightedShare (D9)] unexpected error: %v", err)
	}

	// Send a work notification to a Whatsminer D1 client.
	setMiner(WhatsminerD1)
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case client.ch <- r:
	}

	// Ensure the work notification received is unique to the D1.
	var d1Work []byte
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case d1Work = <-recvCh:
	}
	if bytes.Equal(d1Work, d9Work) {
		t.Fatalf("expected whatsminer d1 work to be different from " +
			"innosilion d9 work")
	}

	// Claim a weighted share for the Whatsminer D1.
	err = client.claimWeightedShare()
	if err != nil {
		t.Fatalf("[claimWeightedShare (D1)] unexpected error: %v", err)
	}

	// Send a work notification to an Antminer DR3 client.
	setMiner(AntminerDR3)
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case client.ch <- r:
	}

	// Ensure the work notification received is unique to the DR3.
	var dr3Work []byte
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case dr3Work = <-recvCh:
	}
	if bytes.Equal(d1Work, dr3Work) {
		t.Fatalf("expected antminer dr3 work to be different from " +
			"whatsminer d1 work")
	}

	// Claim a weighted share for the Antminer DR3.
	err = client.claimWeightedShare()
	if err != nil {
		t.Fatalf("[claimWeightedShare (DR3)] unexpected error: %v", err)
	}

	// Send a work notification to an Antminer DR5 client.
	setMiner(AntminerDR5)
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case client.ch <- r:
	}

	// Ensure the work notification received is identical to that of the DR3.
	var dr5Work []byte
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case dr5Work = <-recvCh:
	}
	if !bytes.Equal(dr5Work, dr3Work) {
		t.Fatalf("expected antminer dr5 work to be equal to antminer dr3 work")
	}

	// Claim a weighted share for the Antminer DR5.
	err = client.claimWeightedShare()
	if err != nil {
		t.Fatalf("[claimWeightedShare (DR5)] unexpected error: %v", err)
	}

	// Send a work notification to an Obelisk DCR1 client.
	setMiner(ObeliskDCR1)
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case client.ch <- r:
	}

	// Ensure the work notification received is unique to the DCR1.
	var dcr1Work []byte
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case dcr1Work = <-recvCh:
	}
	if !bytes.Equal(dr5Work, dcr1Work) {
		t.Fatalf("expected obelisk DCR1 work to be different from " +
			"antminer dr5 work")
	}

	// Claim a weighted share for the Obelisk DCR1.
	err = client.claimWeightedShare()
	if err != nil {
		t.Fatalf("[claimWeightedShare (DCR1)] unexpected error: %v", err)
	}

	// Ensure a CPU client receives an error response when
	// a malformed work submission is sent.
	setMiner(CPU)
	id++
	sub := &Request{
		ID:     &id,
		Method: Submit,
		Params: []string{"tcl", job.UUID, "00000000"},
	}
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	var cpuSub []byte
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case cpuSub = <-recvCh:
	}
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
	if resp.Error == nil {
		t.Fatal("expected a malformed work submission error")
	}

	// Ensure a CPU client receives an error response when
	// submitting work when its exhausted its rate limit.
	client.cfg.WithinLimit = func(ip string, clientType int) bool {
		return false
	}
	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000", "954cee5d", "6ddf0200")
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case cpuSub = <-recvCh:
	}
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
	if resp.Error == nil {
		t.Fatal("expected a rate limit error")
	}
	client.cfg.WithinLimit = func(ip string, clientType int) bool {
		return true
	}

	// Ensure a CPU client receives an error response when
	// submitting work referencing a non-existent job.
	workE = "07000000e2bb3110848ec197118e8df2a3bc85dcaf5a787008a9c70721" +
		"09dfb25e0a000047fe98e377430404709f8045ebf14b3a1903237c2adb49ed55" +
		"72412eb2e0ca3c8ad3ffc23e946e1cce2dca67e2f711a78f41003358630b7923" +
		"1f0af3311bd73c010000000000000000000a000000000064ad2620204e000000" +
		"0000002e0000003b0f000005ec705e0000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000800000010000000000" +
		"0005a0"
	job, err = NewJob(workE, 46)
	if err != nil {
		t.Fatalf("[NewJob] unexpected error: %v", err)
	}
	err = job.Create(client.cfg.DB)
	if err != nil {
		t.Fatalf("failed to persist job %v", err)
	}
	client.extraNonce1 = "b072e5dc"
	id++
	sub = SubmitWorkRequest(&id, "tcl", "notajob", "00000000", "05ec705e", "116f0200")
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case cpuSub = <-recvCh:
	}
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
	if resp.Error == nil {
		t.Fatalf("expected a job not found error")
	}

	// Ensure a non-supported client receives an error response when
	// submitting work.
	setMiner("notaminer")
	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000", "05ec705e", "116f0200")
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case cpuSub = <-recvCh:
	}
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
	if resp.Error == nil {
		t.Fatalf("expected an unknown miner type error")
	}

	// Ensure a CPU client receives an error response if it cannot
	// submit work.
	setMiner(CPU)
	client.cfg.SubmitWork = func(_ context.Context, submission *string) (bool, error) {
		return false, fmt.Errorf("unable to submit work")
	}
	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000", "05ec705e", "116f0200")
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case cpuSub = <-recvCh:
	}
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
	if resp.Error == nil {
		t.Fatalf("expected a submit work error")
	}
	client.cfg.SubmitWork = func(_ context.Context, submission *string) (bool, error) {
		return true, nil
	}

	// Ensure a CPU client receives a non-error response when
	// submitting valid work.
	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000", "05ec705e", "116f0200")
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case cpuSub = <-recvCh:
	}
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
	if resp.Error != nil {
		t.Fatalf("expected a non-error work submission response, got %v", resp.Error)
	}

	// Ensure a CPU client receives an error response when
	// submitting duplicate work.
	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000", "05ec705e", "116f0200")
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case cpuSub = <-recvCh:
	}
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
	if resp.Error == nil {
		t.Fatal("expected a work exists work submission error")
	}
	client.cfg.SubmitWork = func(_ context.Context, submission *string) (bool, error) {
		return false, nil
	}

	// Ensure a CPU client receives an error response when
	// submitting work intended for a different network.
	client.cfg.ActiveNet = chaincfg.MainNetParams()
	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000", "05ec705e", "116f0200")
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case cpuSub = <-recvCh:
	}
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
	if resp.Error == nil {
		t.Fatal("expected a claim work submission error")
	}
	client.cfg.ActiveNet = chaincfg.SimNetParams()

	// Ensure a CPU client receives an error response when
	// submitting work that is rejected by the network.
	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000", "05ec705e", "116f0200")
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case cpuSub = <-recvCh:
	}
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
	if resp.Error != nil {
		t.Fatalf("expected no-error work submission response, got %v", resp.Error)
	}

	// Ensure the pool processes Whatsminer D1 work submissions.
	setMiner(WhatsminerD1)
	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000",
		"954cee5d", "6ddf0200")
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	var d1Sub []byte
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case d1Sub = <-recvCh:
	}
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

	// Ensure the pool processes Antminer DR3 work submissions.
	setMiner(AntminerDR3)
	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000",
		"954cee5d", "6ddf0200")
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	var dr3Sub []byte
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case dr3Sub = <-recvCh:
	}
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

	// Ensure the pool processes Antminer DR5 work submissions.
	setMiner(AntminerDR5)
	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000",
		"954cee5d", "6ddf0200")
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	var dr5Sub []byte
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case dr5Sub = <-recvCh:
	}
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

	// Ensure the pool processes Innosilicon D9 work submissions.
	setMiner(InnosiliconD9)
	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000",
		"954cee5d", "6ddf0200")
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	var d9Sub []byte
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case d9Sub = <-recvCh:
	}
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

	// Ensure the pool processes Obelisk DCR1 work submissions.
	setMiner(ObeliskDCR1)
	id++
	sub = SubmitWorkRequest(&id, "tcl", job.UUID, "00000000",
		"954cee5d", "6ddf0200")
	err = sE.Encode(sub)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	var dcr1Sub []byte
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case dcr1Sub = <-recvCh:
	}
	msg, mType, err = IdentifyMessage(dcr1Sub)
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

	// Fake a bunch of submissions and calculate the hash rate.
	setMiner(CPU)
	atomic.StoreInt64(&client.submissions, 50)
	time.Sleep(time.Millisecond * 1200)
	hash := client.FetchHashRate()
	if hash == ZeroRat {
		t.Fatal("expected a non-nil client hash rate")
	}

	// Ensure the client gets terminated if it sends an unknown message type.
	id++
	r = &Request{
		ID:     &id,
		Method: "unknown",
	}
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}

	// Create a new client connection.
	c, s, err = makeConn(ln, serverCh)
	if err != nil {
		t.Fatalf("[makeConn] unexpected error: %v", err)
	}

	addr = c.RemoteAddr()
	tcpAddr, err = net.ResolveTCPAddr(addr.Network(), addr.String())
	if err != nil {
		t.Fatalf("unable to parse tcp addresss: %v", err)
	}

	cCfg.SoloPool = true
	client, err = NewClient(c, tcpAddr, cCfg)
	if err != nil {
		t.Fatalf("[NewClient] unexpected error: %v", err)
	}

	go client.run(client.ctx)
	time.Sleep(time.Millisecond * 50)

	sE = json.NewEncoder(s)
	sR = bufio.NewReaderSize(s, maxMessageSize)

	go readMsg(client, sR)

	// Ensure a CPU client receives a valid non-error response when
	// a valid authorize request is sent.
	id++
	r = AuthorizeRequest(&id, "mn", "SsiuwSRYvH7pqWmRxFJWR8Vmqc3AWsjmK2Y")
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case data = <-recvCh:
	}
	msg, mType, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}
	if mType != ResponseMessage {
		t.Fatalf("expected an auth response message (%d), got %d", mType, msg.MessageType())
	}
	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("expected response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.ID != *r.ID {
		t.Fatalf("expected response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("expected non-error authorize response, got %v", resp.Error)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case data = <-recvCh:
	}
	msg, mType, err = IdentifyMessage(data)
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
	if req.Method != SetDifficulty {
		t.Fatalf("expected %s message method, got %s", SetDifficulty, req.Method)
	}

	// Ensure a CPU client receives a valid non-error response when
	// a valid subscribe request is sent.
	id++
	r = SubscribeRequest(&id, "mcpu", "1.0.1", "")
	err = sE.Encode(r)
	if err != nil {
		t.Fatalf("[Encode] unexpected error: %v", err)
	}
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case data = <-recvCh:
	}
	msg, mType, err = IdentifyMessage(data)
	if err != nil {
		t.Fatalf("[IdentifyMessage] unexpected error: %v", err)
	}
	if mType != ResponseMessage {
		t.Fatalf("expected a subscribe response message, got %v", mType)
	}
	resp, ok = msg.(*Response)
	if !ok {
		t.Fatalf("expected subsribe response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.ID != *r.ID {
		t.Fatalf("expected suscribe response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("expected non-error subscribe response, got %v", resp.Error)
	}

	// Ensure the CPU client is now authorized and subscribed
	// for work updates.
	client.authorizedMtx.Lock()
	authorized = client.authorized
	client.authorizedMtx.Unlock()

	if !authorized {
		t.Fatalf("expected an authorized mining client")
	}

	client.subscribedMtx.Lock()
	subscribed = client.subscribed
	client.subscribedMtx.Unlock()

	if !subscribed {
		t.Fatalf("expected a subscribed mining client")
	}

	// Trigger time-rolled work updates to the CPU  client.
	setCurrentWork(workE)

	// Send a work notification to the CPU client.
	r = WorkNotification(job.UUID, prevBlock, genTx1, genTx2,
		blockVersion, nBits, nTime, true)
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case client.ch <- r:
	}

	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case cpuWork = <-recvCh:
	}
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

	// Ensure the client receives time-rolled work.
	var timeRolledWork []byte
	select {
	case <-client.ctx.Done():
		t.Fatalf("client context done: %v", err)
	case timeRolledWork = <-recvCh:
	}
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

	// Trigger a client timeout by waiting.
	time.Sleep(time.Millisecond * 1500)

	// Empty the job bucket.
	err = emptyBucket(db, jobBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Empty the accepted work bucket.
	err = emptyBucket(db, workBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	client.cfg.EndpointWg.Wait()
}
