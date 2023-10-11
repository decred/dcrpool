// Copyright (c) 2021-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bufio"
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

	errs "github.com/decred/dcrpool/errors"
)

var (
	currentWork    string
	currentWorkMtx sync.RWMutex

	powLimit    = chaincfg.SimNetParams().PowLimit
	iterations  = math.Pow(2, float64(256-powLimit.BitLen()))
	maxGenTime  = time.Millisecond * 500
	cTimeout    = time.Millisecond * 2000
	hashCalcMax = time.Millisecond * 1500
	poolDiffs   = NewDifficultySet(chaincfg.SimNetParams(),
		new(big.Rat).SetInt(powLimit), maxGenTime)
	config = &ClientConfig{
		ActiveNet:       chaincfg.SimNetParams(),
		NonceIterations: iterations,
		MaxGenTime:      maxGenTime,
		FetchMinerDifficulty: func(miner string) (*DifficultyInfo, error) {
			return poolDiffs.fetchMinerDifficulty(miner)
		},
		SoloPool: false,
		SubmitWork: func(_ context.Context, submission string) (bool, error) {
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
		HashCalcThreshold: hashCalcMax,
		ClientTimeout:     cTimeout,
		SignalCache: func(_ CacheUpdateEvent) {
			// Do nothing.
		},
		MonitorCycle:    time.Minute,
		MaxUpgradeTries: 5,
		RollWorkCycle:   rollWorkCycle,
	}
	userAgent = func(miner, version string) string {
		return fmt.Sprintf("%s/%s", miner, version)
	}
)

func splitMinerID(id string) (string, string) {
	const separator = "/"
	split := strings.Split(id, separator)
	return split[0], split[1]
}

func setCurrentWork(work string) {
	currentWorkMtx.Lock()
	currentWork = work
	currentWorkMtx.Unlock()
}

func setMiner(c *Client, m string) error {
	info, err := c.cfg.FetchMinerDifficulty(m)
	if err != nil {
		return err
	}

	c.mtx.Lock()
	c.miner = m
	c.id = fmt.Sprintf("%v/%v", c.extraNonce1, c.miner)
	c.diffInfo = info
	c.mtx.Unlock()

	return nil
}

func fetchMiner(c *Client) string {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.miner
}

func readMsg(c *Client, r *bufio.Reader, recvCh chan []byte) {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			// Non-blocking receive fallthrough.
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

func acceptConn(ln *net.TCPListener, serverCh chan net.Conn) {
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
}

func setup(ctx context.Context, cfg *ClientConfig) (*json.Encoder, *net.TCPListener, *Client, chan net.Conn, chan []byte, error) {
	port := uint32(3030)
	laddr, err := net.ResolveTCPAddr("tcp",
		fmt.Sprintf("%s:%d", "127.0.0.1", port))
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	ln, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	serverCh := make(chan net.Conn)
	go acceptConn(ln, serverCh)

	// Create a new client connection.
	c, s, err := makeConn(ln, serverCh)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	addr := c.RemoteAddr()
	tcpAddr, err := net.ResolveTCPAddr(addr.Network(), addr.String())
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	cfg.db = db
	client, err := NewClient(ctx, c, tcpAddr, cfg)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	go client.run()
	time.Sleep(time.Millisecond * 50)
	sE := json.NewEncoder(s)
	sR := bufio.NewReaderSize(s, maxMessageSize)

	recvCh := make(chan []byte, 5)
	go readMsg(client, sR, recvCh)

	return sE, ln, client, serverCh, recvCh, nil
}

func testClientMessageHandling(t *testing.T) {
	ctx := context.Background()
	cfg := *config
	cfg.RollWorkCycle = time.Minute * 5 // Avoiding rolled work for this test.
	sE, ln, client, _, recvCh, err := setup(ctx, &cfg)
	if err != nil {
		t.Fatalf("[setup] unexpected error: %v", err)
	}

	defer ln.Close()

	err = setMiner(client, CPU)
	if err != nil {
		t.Fatalf("unexpected set miner error: %v", err)
	}

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
		t.Fatalf("expected response, got %T", msg)
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
		t.Fatalf("expected response, got %T", msg)
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
		t.Fatalf("expected response, got %T", msg)
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
		t.Fatalf("expected response, got %T", msg)
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

	// Ensure a CPU client receives a valid non-error response when a
	// valid subscribe request is sent.
	err = setMiner(client, CPU)
	if err != nil {
		t.Fatalf("unexpected set miner error: %v", err)
	}

	id++
	cpu, cpuVersion := splitMinerID(cpuID)
	r = SubscribeRequest(&id, userAgent(cpu, cpuVersion), "")
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
		t.Fatalf("expected subscribe response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.ID != *r.ID {
		t.Fatalf("expected subscribe response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("expected a non-error subscribe response, got %v", resp.Error)
	}

	// Ensure the CPU client is now authorized and subscribed
	// for work updates.
	client.statusMtx.RLock()
	authorized := client.authorized
	subscribed := client.subscribed
	client.statusMtx.RUnlock()

	if !authorized {
		t.Fatalf("expected an authorized mining client")
	}

	if !subscribed {
		t.Fatalf("expected a subscribed mining client")
	}

	const workE = "07000000022b580ca96146e9c85fa1ee2ec02e0e2579af4e3881fc619e" +
		"c52d64d83e0000bd646e312ff574bc90e08ed91f1d99a85b318cb4464f2a24f9ad2b" +
		"f3b9881c2bc9c344adde75e89b14b627acce606e6d652915bdb71dcf5351e8ad6128" +
		"faab9e010000000000000000000000000000003e133920204e000000000000290000" +
		"00a6030000954cee5d00000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000008000000100000000000005a0"
	job := NewJob(workE, 41)
	err = client.cfg.db.persistJob(job)
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
	if !errors.Is(err, errs.ClaimShare) {
		t.Fatalf("[claimWeightedShare (CPU)] expected a solo pool mode error")
	}
	client.cfg.SoloPool = false
	client.cfg.ActiveNet = chaincfg.MainNetParams()
	err = client.claimWeightedShare()
	if !errors.Is(err, errs.ClaimShare) {
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

	// Ensure a CPU client receives an error response when
	// a malformed work submission is sent.
	err = setMiner(client, CPU)
	if err != nil {
		t.Fatalf("unexpected set miner error: %v", err)
	}

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
	const workE2 = "07000000e2bb3110848ec197118e8df2a3bc85dcaf5a787008a9c7072" +
		"109dfb25e0a000047fe98e377430404709f8045ebf14b3a1903237c2adb49ed55724" +
		"12eb2e0ca3c8ad3ffc23e946e1cce2dca67e2f711a78f41003358630b79231f0af33" +
		"11bd73c010000000000000000000a000000000064ad2620204e0000000000002e000" +
		"0003b0f000005ec705e0000000000000000000000000000000000000000000000000" +
		"00000000000000000000000000000008000000100000000000005a0"
	job = NewJob(workE2, 46)
	err = client.cfg.db.persistJob(job)
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

	// Ensure a non-supported miner id cannot be set for a client.
	err = setMiner(client, "notaminer")
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected a set miner error: %v", err)
	}

	// Ensure a CPU client receives an error response if it cannot
	// submit work.
	err = setMiner(client, CPU)
	if err != nil {
		t.Fatalf("unexpected set miner error: %v", err)
	}
	client.cfg.SubmitWork = func(_ context.Context, submission string) (bool, error) {
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
	client.cfg.SubmitWork = func(_ context.Context, submission string) (bool, error) {
		return true, nil
	}

	setCurrentWork(workE2)

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
		t.Fatalf("expected a response message, got %v, %v", mType, msg.String())
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
	client.cfg.SubmitWork = func(_ context.Context, submission string) (bool, error) {
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

	client.cancel()
}

func testClientHashCalc(t *testing.T) {
	ctx := context.Background()
	cfg := *config
	cfg.RollWorkCycle = time.Minute * 5 // Avoiding rolled work for this test.
	_, ln, client, _, _, err := setup(ctx, &cfg)
	if err != nil {
		t.Fatalf("[setup] unexpected error: %v", err)
	}

	err = setMiner(client, CPU)
	if err != nil {
		t.Fatalf("unexpected set miner error: %v", err)
	}

	defer ln.Close()

	// Fake a bunch of submissions and calculate the hash rate.
	atomic.StoreInt64(&client.submissions, 50)
	time.Sleep(hashCalcMax + (hashCalcMax / 4))
	hash := client.FetchHashRate()
	if hash == ZeroRat {
		t.Fatal("expected a non-nil client hash rate")
	}

	client.cancel()
}

func testClientTimeRolledWork(t *testing.T) {
	ctx := context.Background()
	cfg := *config
	cfg.RollWorkCycle = time.Millisecond * 200
	sE, ln, client, _, recvCh, err := setup(ctx, &cfg)
	if err != nil {
		t.Fatalf("[setup] unexpected error: %v", err)
	}

	err = setMiner(client, CPU)
	if err != nil {
		t.Fatalf("unexpected set miner error: %v", err)
	}

	defer ln.Close()

	var msg Message
	var mType int
	var data []byte

	// Ensure a CPU client receives a valid non-error response when
	// a valid authorize request is sent.
	id := uint64(1)
	r := AuthorizeRequest(&id, "mn", "SsiuwSRYvH7pqWmRxFJWR8Vmqc3AWsjmK2Y")
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
	resp, ok := msg.(*Response)
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

	// Ensure a CPU client receives a valid non-error response when
	// a valid subscribe request is sent.
	id++
	cpu, cpuVersion := splitMinerID(cpuID)
	r = SubscribeRequest(&id, userAgent(cpu, cpuVersion), "")
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
		t.Fatalf("expected subscribe response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.ID != *r.ID {
		t.Fatalf("expected subscribe response with id %d, got %d", *r.ID, resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("expected non-error subscribe response, got %v", resp.Error)
	}

	// Ensure the CPU client is now authorized and subscribed
	// for work updates.
	client.statusMtx.RLock()
	authorized := client.authorized
	subscribed := client.subscribed
	client.statusMtx.RUnlock()

	if !authorized {
		t.Fatalf("expected an authorized mining client")
	}

	if !subscribed {
		t.Fatalf("expected a subscribed mining client")
	}

	const workE = "07000000022b580ca96146e9c85fa1ee2ec02e0e2579af4e3881fc619e" +
		"c52d64d83e0000bd646e312ff574bc90e08ed91f1d99a85b318cb4464f2a24f9ad2b" +
		"f3b9881c2bc9c344adde75e89b14b627acce606e6d652915bdb71dcf5351e8ad6128" +
		"faab9e010000000000000000000000000000003e133920204e000000000000290000" +
		"00a6030000954cee5d00000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000008000000100000000000005a0"

	// Trigger time-rolled work updates to the CPU client.
	setCurrentWork(workE)

	minutesAgo := time.Now().Add(-time.Minute * 5)
	atomic.StoreInt64(&client.lastWorkTime, minutesAgo.Unix())

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

	client.cancel()
}

func testClientUpgrades(t *testing.T) {
	// Create mock upgrade path for CPU mining.
	const clientCPU2 = CPU + "2"
	activeNet := config.ActiveNet
	mockPoolDiffs := func() map[string]*DifficultyInfo {
		const maxGenTime = time.Millisecond * 500
		genTime := new(big.Int).SetInt64(int64(maxGenTime.Seconds()))
		return map[string]*DifficultyInfo{
			CPU: func() *DifficultyInfo {
				hashRate := minerHashes[CPU]
				target, diff := calculatePoolTarget(activeNet, hashRate, genTime)
				return &DifficultyInfo{
					target:     target,
					difficulty: diff,
					powLimit:   new(big.Rat).SetInt(activeNet.PowLimit),
				}
			}(),
			clientCPU2: func() *DifficultyInfo {
				hashRate := new(big.Int).Mul(minerHashes[CPU], big.NewInt(2))
				target, diff := calculatePoolTarget(activeNet, hashRate, genTime)
				return &DifficultyInfo{
					target:     target,
					difficulty: diff,
					powLimit:   new(big.Rat).SetInt(activeNet.PowLimit),
				}
			}(),
		}
	}()
	fetchMinerDifficulty := func(miner string) (*DifficultyInfo, error) {
		diffData, ok := mockPoolDiffs[miner]
		if !ok {
			desc := fmt.Sprintf("no difficulty data found for miner %s", miner)
			return nil, errs.PoolError(errs.ValueNotFound, desc)
		}
		return diffData, nil
	}

	ctx := context.Background()
	cfg := *config
	cfg.RollWorkCycle = time.Minute * 5 // Avoiding rolled work for this test.
	cfg.MaxUpgradeTries = 2
	cfg.MonitorCycle = time.Millisecond * 100
	cfg.ClientTimeout = time.Millisecond * 300
	cfg.FetchMinerDifficulty = fetchMinerDifficulty
	_, ln, client, _, _, err := setup(ctx, &cfg)
	if err != nil {
		ln.Close()
		t.Fatalf("[setup] unexpected error: %v", err)
	}

	err = setMiner(client, CPU)
	if err != nil {
		ln.Close()
		t.Fatalf("unexpected set miner error: %v", err)
	}

	const minerIdx = 0
	idPair := newMinerIDPair(cpuID, CPU, clientCPU2)

	// Trigger a client upgrade.
	atomic.StoreInt64(&client.submissions, 50)

	go client.monitor(minerIdx, idPair, cfg.MonitorCycle, cfg.MaxUpgradeTries)
	time.Sleep(cfg.MonitorCycle + (cfg.MonitorCycle / 2))

	if fetchMiner(client) != clientCPU2 {
		ln.Close()
		t.Fatalf("expected a miner id of %s, got %s", clientCPU2, client.miner)
	}

	client.cancel()

	ln.Close()

	// Ensure the client upgrade fails after max tries.
	_, ln, client, _, _, err = setup(ctx, &cfg)
	if err != nil {
		ln.Close()
		t.Fatalf("[setup] unexpected error: %v", err)
	}

	defer ln.Close()

	err = setMiner(client, CPU)
	if err != nil {
		t.Fatalf("unexpected set miner error: %v", err)
	}

	atomic.StoreInt64(&client.submissions, 2)

	go client.monitor(minerIdx, idPair, cfg.MonitorCycle, cfg.MaxUpgradeTries)
	time.Sleep(cfg.MonitorCycle + (cfg.MonitorCycle / 2))

	if fetchMiner(client) == CPU {
		t.Fatalf("expected a miner of %s, got %s", CPU, client.miner)
	}

	// Trigger a client timeout by waiting.
	time.Sleep(cTimeout + (cTimeout / 4))

	client.cancel()
}
