// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
)

const (
	// MaxMessageSize represents the maximum size of a transmitted message
	// allowed, in bytes.
	MaxMessageSize = 250

	// hashCalcThreshold represents the minimum operation time in seconds
	// before a client's hash rate is calculated.
	hashCalcThreshold = 300
)

var (
	// ZeroInt is the default value for a big.Int.
	ZeroInt = new(big.Int).SetInt64(0)

	// ZeroRat is the default value for a big.Rat.
	ZeroRat = new(big.Rat).SetInt64(0)
)

// readPayload is a convenience type that wraps a message and its
// associated type.
type readPayload struct {
	msg     Message
	msgType int
}

// hexReversed reverses a hex string.
func hexReversed(in string) (string, error) {
	if len(in)%2 != 0 {
		desc := fmt.Sprintf("expected even hex input length, got %d", len(in))
		return "", MakeError(ErrWrongInputLength, desc, nil)
	}

	buf := bytes.NewBufferString("")
	for i := len(in) - 1; i > -1; i -= 2 {
		buf.WriteByte(in[i-1])
		buf.WriteByte(in[i])
	}
	return buf.String(), nil
}

// reversePrevBlockWords reverses each 4-byte word in the provided hex encoded
// previous block hash.
func reversePrevBlockWords(hashE string) string {
	buf := bytes.NewBufferString("")
	for i := 0; i < len(hashE); i += 8 {
		buf.WriteString(hashE[i+6 : i+8])
		buf.WriteString(hashE[i+4 : i+6])
		buf.WriteString(hashE[i+2 : i+4])
		buf.WriteString(hashE[i : i+2])
	}
	return buf.String()
}

// Client represents a client connection.
type Client struct {
	submissions int64 // update atomically
	conn        net.Conn
	endpoint    *endpoint
	encoder     *json.Encoder
	reader      *bufio.Reader
	ctx         context.Context
	cancel      context.CancelFunc
	addr        *net.TCPAddr
	name        string
	extraNonce1 string
	ch          chan Message
	readCh      chan readPayload
	req         map[uint64]string
	reqMtx      sync.RWMutex
	account     string
	authorized  bool
	subscribed  bool
	hashRate    *big.Rat
	hashRateMtx sync.RWMutex
	wg          sync.WaitGroup
}

// generateExtraNonce1 generates a random 4-byte extraNonce1 for the
// client.
func (c *Client) generateExtraNonce1() error {
	id := make([]byte, 4)
	_, err := rand.Read(id)
	if err != nil {
		return err
	}

	c.extraNonce1 = hex.EncodeToString(id)

	return nil
}

// NewClient creates client connection instance.
func NewClient(conn net.Conn, endpoint *endpoint, addr *net.TCPAddr) (*Client, error) {
	ctx, cancel := context.WithCancel(context.TODO())
	c := &Client{
		conn:     conn,
		endpoint: endpoint,
		ctx:      ctx,
		cancel:   cancel,
		ch:       make(chan Message),
		readCh:   make(chan readPayload),
		encoder:  json.NewEncoder(conn),
		reader:   bufio.NewReaderSize(conn, MaxMessageSize),
		addr:     addr,
		hashRate: ZeroRat,
	}

	err := c.generateExtraNonce1()
	if err != nil {
		return nil, err
	}

	return c, nil
}

// generateID creates a unique id of for the pool client.
func (c *Client) generateID() string {
	return fmt.Sprintf("%v/%v", c.extraNonce1, c.endpoint.miner)
}

// fetchStratumMethod fetches the method of the associated request.
func (c *Client) fetchStratumMethod(id uint64) string {
	c.reqMtx.RLock()
	method := c.req[id]
	c.reqMtx.RUnlock()
	return method
}

// shutdown terminates all client processes and established connections.
func (c *Client) shutdown() {
	addr := c.addr
	c.endpoint.hub.connectionsMtx.Lock()
	c.endpoint.hub.connections[addr.IP.String()]--
	c.endpoint.hub.connectionsMtx.Unlock()

	c.endpoint.hub.limiter.RemoveLimiter(addr.String())
	c.endpoint.removeClient(c)
	log.Tracef("Connection to (%v) terminated.", c.generateID())
}

// claimWeightedShare records a weighted share for the pool client. This
// serves as proof of verifiable work contributed to the mining pool.
func (c *Client) claimWeightedShare() error {
	if c.endpoint.hub.cfg.ActiveNet.Name == chaincfg.MainNetParams.Name &&
		c.endpoint.miner == CPU {
		log.Error("CPU miners are reserved for only simnet testing purposes")
		return nil
	}

	weight := ShareWeights[c.endpoint.miner]
	share := NewShare(c.account, weight)
	err := share.Create(c.endpoint.hub.db)
	if err != nil {
		return err
	}

	log.Tracef("Weighted share of %v for pool client %v claimed",
		weight, c.generateID())
	return nil
}

// handleAuthorizeRequest processes authorize request messages received.
func (c *Client) handleAuthorizeRequest(req *Request, allowed bool) {
	if !allowed {
		log.Errorf("unable to process authorize request, limit reached")
		err := NewStratumError(Unknown, nil)
		resp := AuthorizeResponse(*req.ID, false, err)
		c.ch <- resp
		return
	}

	// The client's username is expected to be of the format address.clientid
	// when in pool mining mode. For solo pool mode the username expected is
	// just the client's id.
	username, err := ParseAuthorizeRequest(req)
	if err != nil {
		log.Errorf("unable to parse authorize request: %v", err)
		err := NewStratumError(Unknown, nil)
		resp := AuthorizeResponse(*req.ID, false, err)
		c.ch <- resp
		return
	}

	if !c.endpoint.hub.cfg.SoloPool {
		parts := strings.Split(username, ".")
		if len(parts) != 2 {
			log.Errorf("Invalid username format, expected `address.clientid`,"+
				"got %v", username)
			err := NewStratumError(Unknown, nil)
			resp := AuthorizeResponse(*req.ID, false, err)
			c.ch <- resp
			return
		}

		name := strings.TrimSpace(parts[1])
		address := strings.TrimSpace(parts[0])

		// Ensure the provided address is valid and associated with the active
		// network.
		addr, err := dcrutil.DecodeAddress(address)
		if err != nil {
			log.Errorf("unable to decode address: %v", err)
			err := NewStratumError(Unknown, nil)
			resp := AuthorizeResponse(*req.ID, false, err)
			c.ch <- resp
			return
		}

		if !addr.IsForNet(c.endpoint.hub.cfg.ActiveNet) {
			log.Errorf("Address %s is not associated with the active "+
				"network %s", address, c.endpoint.hub.cfg.ActiveNet.Name)
			err := NewStratumError(Unknown, nil)
			resp := AuthorizeResponse(*req.ID, false, err)
			c.ch <- resp
			return
		}

		id, err := AccountID(address)
		if err != nil {
			log.Errorf("unable to generate account id: %v", err)
			err := NewStratumError(Unknown, nil)
			resp := AuthorizeResponse(*req.ID, false, err)
			c.ch <- resp
			return
		}

		_, err = FetchAccount(c.endpoint.hub.db, []byte(id))
		if err != nil {
			if !IsError(err, ErrValueNotFound) {
				log.Errorf("unable to fetch account: %v", err)
				err := NewStratumError(Unknown, nil)
				resp := AuthorizeResponse(*req.ID, false, err)
				c.ch <- resp
				return
			}
		}

		// Create the account if it does not already exist.
		account, err := NewAccount(address)
		if err != nil {
			log.Errorf("unable to create account: %v", err)
			err := NewStratumError(Unknown, nil)
			resp := AuthorizeResponse(*req.ID, false, err)
			c.ch <- resp
			return
		}

		err = account.Create(c.endpoint.hub.db)
		if err != nil {
			log.Errorf("unable to persist account: %v", err)
			err := NewStratumError(Unknown, nil)
			resp := AuthorizeResponse(*req.ID, false, err)
			c.ch <- resp
			return
		}

		c.account = id
		c.name = name
	}

	if c.endpoint.hub.cfg.SoloPool {
		c.name = username
	}

	c.authorized = true
	resp := AuthorizeResponse(*req.ID, true, nil)
	c.ch <- resp
}

// handleSubscribeRequest processes subscription request messages received.
func (c *Client) handleSubscribeRequest(req *Request, allowed bool) {
	if !allowed {
		log.Errorf("unable to process subscribe request, limit reached")
		err := NewStratumError(Unknown, nil)
		resp := SubscribeResponse(*req.ID, "", "", 0, err)
		c.ch <- resp
		return
	}

	_, nid, err := ParseSubscribeRequest(req)
	if err != nil {
		log.Errorf("unable to parse subscribe request: %v", err)
		err := NewStratumError(Unknown, nil)
		resp := SubscribeResponse(*req.ID, "", "", 0, err)
		c.ch <- resp
		return
	}

	if nid == "" {
		nid = fmt.Sprintf("mn%v", c.extraNonce1)
	}

	var resp *Response
	switch c.endpoint.miner {
	case AntminerDR3, AntminerDR5:
		// The DR5 and DR3 are not fully complaint with the stratum spec.
		// They use an 8-byte extraNonce2 regardless of the
		// extraNonce2Size provided.
		//
		// The extraNonce1 is appended to the extraNonce2 in the
		// extraNonce2 value returned in mining.submit. As a result,
		// the extraNonce1 sent in mining.subscribe response is formatted as:
		// 	extraNonce2 space (8-byte) + miner's extraNonce1 (4-byte)
		paddedExtraNonce1 := strings.Repeat("0", 16) + c.extraNonce1

		resp = SubscribeResponse(*req.ID, nid, paddedExtraNonce1, 8, nil)
		log.Tracef("Subscribe response is: %v", spew.Sdump(resp))

	case WhatsminerD1:
		// The D1 is not fully complaint with the stratum spec.
		// It uses a 4-byte extraNonce2 regardless of the
		// extraNonce2Size provided.
		//
		// The extraNonce1 is appended to the extraNonce2 in the
		// extraNonce2 value returned in mining.submit. As a result,
		// the extraNonce1 sent in mining.subscribe response is formatted as:
		// 	extraNonce2 space (4-byte) + miner's extraNonce1 (4-byte)
		paddedExtraNonce1 := strings.Repeat("0", 8) + c.extraNonce1

		resp = SubscribeResponse(*req.ID, nid, paddedExtraNonce1,
			ExtraNonce2Size, nil)
		log.Tracef("Subscribe response is: %v", spew.Sdump(resp))

	default:
		resp = SubscribeResponse(*req.ID, nid, c.extraNonce1,
			ExtraNonce2Size, nil)
		log.Tracef("Subscribe response is: %v", spew.Sdump(resp))
	}

	c.ch <- resp
	c.subscribed = true
}

// setDifficulty sends the pool client's difficulty ratio.
func (c *Client) setDifficulty() {
	log.Tracef("Difficulty is %v", c.endpoint.diffData.difficulty.FloatString(4))
	diffNotif := SetDifficultyNotification(c.endpoint.diffData.difficulty)
	c.ch <- diffNotif
}

// handleSubmitWorkRequest processes work submission request messages received.
func (c *Client) handleSubmitWorkRequest(req *Request, allowed bool) {
	if !allowed {
		log.Errorf("unable to process submit work request, limit reached")
		err := NewStratumError(Unknown, nil)
		resp := SubmitWorkResponse(*req.ID, false, err)
		c.ch <- resp
		return
	}

	log.Tracef("Received work submission from %s is: %v",
		c.generateID(), spew.Sdump(req))

	_, jobID, extraNonce2E, nTimeE, nonceE, err := ParseSubmitWorkRequest(req,
		c.endpoint.miner)
	if err != nil {
		log.Errorf("unable to parse submit work request: %v", err)
		err := NewStratumError(Unknown, nil)
		resp := SubmitWorkResponse(*req.ID, false, err)
		c.ch <- resp
		return
	}

	job, err := FetchJob(c.endpoint.hub.db, []byte(jobID))
	if err != nil {
		log.Errorf("unable to fetch job: %v", err)
		err := NewStratumError(Unknown, nil)
		resp := SubmitWorkResponse(*req.ID, false, err)
		c.ch <- resp
		return
	}

	header, err := GenerateSolvedBlockHeader(job.Header,
		c.extraNonce1, extraNonce2E, nTimeE, nonceE, c.endpoint.miner)
	if err != nil {
		log.Errorf("unable to generate solved block header: %v", err)
		err := NewStratumError(Unknown, nil)
		resp := SubmitWorkResponse(*req.ID, false, err)
		c.ch <- resp
		return
	}

	log.Tracef("Submitted work from %s is: %v", c.generateID(),
		spew.Sdump(header))
	log.Tracef("Submited work hash at height #%v is %s", header.Height,
		header.BlockHash().String())

	diffData := c.endpoint.diffData
	poolTarget := diffData.target
	target := new(big.Rat).SetInt(blockchain.CompactToBig(header.Bits))

	// The target difficulty must be larger than zero.
	if target.Sign() <= 0 {
		log.Errorf("block target difficulty of %064x is too "+
			"low", target)
		err := NewStratumError(Unknown, nil)
		resp := SubmitWorkResponse(*req.ID, false, err)
		c.ch <- resp
		return
	}

	hash := header.BlockHash()
	hashTarget := new(big.Rat).SetInt(blockchain.HashToBig(&hash))

	netDiff := new(big.Rat).Quo(diffData.powLimit, poolTarget)
	hashDiff := new(big.Rat).Quo(diffData.powLimit, hashTarget)

	log.Tracef("network difficulty is: %s", netDiff.FloatString(4))
	log.Tracef("pool difficulty is: %s", diffData.difficulty.FloatString(4))
	log.Tracef("hash difficulty is: %s", hashDiff.FloatString(4))

	// Only submit work to the network if the submitted blockhash is
	// less than the pool target for the client.
	if hashTarget.Cmp(poolTarget) > 0 {
		log.Errorf("submitted work from %s is not less than its "+
			"corresponding pool target", c.generateID())
		err := NewStratumError(LowDifficultyShare, nil)
		resp := SubmitWorkResponse(*req.ID, false, err)
		c.ch <- resp
		return
	}

	atomic.AddInt64(&c.submissions, 1)

	// Claim a weighted share for work contributed to the pool if not mining
	// in solo mining mode.
	if !c.endpoint.hub.cfg.SoloPool {
		err := c.claimWeightedShare()
		if err != nil {
			log.Errorf("failed to persist weighted share for %v: %v",
				c.generateID(), err)
			err := NewStratumError(Unknown, nil)
			resp := SubmitWorkResponse(*req.ID, false, err)
			c.ch <- resp
			return
		}
	}

	// Only submit work to the network if the submitted blockhash is
	// less than the network target difficulty.
	if hashTarget.Cmp(target) > 0 {
		log.Tracef("submitted work from %s is not less than the "+
			"network target difficulty", c.generateID())
		resp := SubmitWorkResponse(*req.ID, true, nil)
		c.ch <- resp
		return
	}

	// Generate and send the work submission.
	headerB, err := header.Bytes()
	if err != nil {
		log.Errorf("unable to fetch block header bytes: %v", err)
		err := NewStratumError(Unknown, nil)
		resp := SubmitWorkResponse(*req.ID, false, err)
		c.ch <- resp
		return
	}

	submissionB := make([]byte, getworkDataLen)
	copy(submissionB[:wire.MaxBlockHeaderPayload], headerB)
	copy(submissionB[wire.MaxBlockHeaderPayload:],
		c.endpoint.hub.blake256Pad)
	submission := hex.EncodeToString(submissionB)
	accepted, err := c.endpoint.hub.SubmitWork(&submission)
	if err != nil {
		log.Errorf("unable to submit work request: %v", err)
		err := NewStratumError(Unknown, nil)
		resp := SubmitWorkResponse(*req.ID, false, err)
		c.ch <- resp
		return
	}

	// Create accepted work if the work submission is accepted by the mining node.
	if accepted {
		work := NewAcceptedWork(hash.String(), header.PrevBlock.String(),
			header.Height, c.account, c.endpoint.miner)
		err := work.Create(c.endpoint.hub.db)
		if err != nil {
			// If the submitted accetped work already exists, ignore the
			// submission.
			if IsError(err, ErrWorkExists) {
				log.Tracef("Work already exists, ignoring.")
				err := NewStratumError(DuplicateShare, nil)
				resp := SubmitWorkResponse(*req.ID, false, err)
				c.ch <- resp
				return
			}

			log.Errorf("unable to persist accepted work: %v", err)
			err := NewStratumError(Unknown, nil)
			resp := SubmitWorkResponse(*req.ID, false, err)
			c.ch <- resp
			return
		}

		log.Tracef("Work accepted by the network")
		return
	}

	if !accepted {
		log.Tracef("Work rejected by the network")
		c.ch <- SubmitWorkResponse(*req.ID, false, nil)
		return
	}
}

// read receives incoming data and passes the message received for
// processing. This must be run as goroutine.
func (c *Client) read() {
	for {
		err := c.conn.SetDeadline(time.Now().Add(time.Minute * 4))
		if err != nil {
			log.Errorf("%s: unable to set deadline: %v", c.generateID(), err)
			c.cancel()
			return
		}

		data, err := c.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				c.cancel()
				return
			}

			nErr, ok := err.(*net.OpError)
			if !ok {
				log.Errorf("%s: failed to read bytes: %v", c.generateID(), err)
				c.cancel()
				return
			}

			if nErr != nil {
				if nErr.Op == "read" && nErr.Net == "tcp" {
					switch {
					case nErr.Timeout():
						log.Errorf("%s: read timeout: %v", c.generateID(), err)
					case !nErr.Timeout():
						log.Errorf("%s: read error: %v", c.generateID(), err)
					}

					c.cancel()
					return
				}
			}

			log.Errorf("failed to read bytes: %v %T", err, err)
			c.cancel()
			return
		}

		msg, reqType, err := IdentifyMessage(data)
		if err != nil {
			log.Errorf("unable to identify message: %v", err)
			c.conn.Close()
			c.cancel()
			return
		}

		log.Tracef("Message received from %s is: %v", c.generateID(),
			spew.Sdump(data))

		c.readCh <- readPayload{msg, reqType}
	}
}

// updateWork updates a client with a timestamp-rolled current work.
// This should be called after a client completes a work submission or
// afterr client authentication.
func (c *Client) updateWork(allowed bool) {
	if !allowed {
		return
	}

	currWorkE := c.endpoint.hub.getCurrentWork()
	if currWorkE == "" {
		return
	}

	now := uint32(time.Now().Unix())

	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, now)
	timestampE := hex.EncodeToString(b)

	buf := bytes.NewBufferString("")
	buf.WriteString(currWorkE[:272])
	buf.WriteString(timestampE)
	buf.WriteString(currWorkE[280:])

	updatedWorkE := buf.String()

	blockVersion := updatedWorkE[:8]
	prevBlock := updatedWorkE[8:72]
	genTx1 := updatedWorkE[72:288]
	nBits := updatedWorkE[232:240]
	nTime := updatedWorkE[272:280]
	genTx2 := updatedWorkE[352:360]

	heightD, err := hex.DecodeString(updatedWorkE[256:264])
	if err != nil {
		log.Errorf("failed to decode block height %s: %v", string(heightD), err)
	}

	height := binary.LittleEndian.Uint32(heightD)

	// Create a job for the timestamp-rolled current work.
	job, err := NewJob(updatedWorkE, height)
	if err != nil {
		log.Errorf("failed to create job: %v", err)
		return
	}

	err = job.Create(c.endpoint.hub.db)
	if err != nil {
		log.Errorf("failed to persist job: %v", err)
		return
	}

	workNotif := WorkNotification(job.UUID, prevBlock, genTx1, genTx2,
		blockVersion, nBits, nTime, true)

	select {
	case c.ch <- workNotif:
		log.Tracef("Sent a timestamp-rolled current work at "+
			"height #%v to %v: %v", height, c.generateID(), updatedWorkE)
	default:
	}
}

// process  handles incoming messages from the connected pool client.
// It must be run as a goroutine.
func (c *Client) process(ctx context.Context) {
	ip := c.addr.String()

	for {
		select {
		case <-ctx.Done():
			c.wg.Done()
			return

		case payLoad := <-c.readCh:
			msg := payLoad.msg
			msgType := payLoad.msgType

			// Ensure the requesting client is within their request limits.
			allowed := c.endpoint.hub.limiter.WithinLimit(ip, PoolClient)

			switch msgType {
			case RequestMessage:
				req := msg.(*Request)
				switch req.Method {
				case Authorize:
					c.handleAuthorizeRequest(req, allowed)
					c.setDifficulty()
					time.Sleep(time.Second * 2)
					c.updateWork(allowed)

				case Subscribe:
					c.handleSubscribeRequest(req, allowed)

				case Submit:
					c.handleSubmitWorkRequest(req, allowed)
					c.updateWork(allowed)

				default:
					log.Errorf("unknown request method for request: %s", req.Method)
					c.conn.Close()
					c.cancel()
					continue
				}

			case ResponseMessage:
				resp := msg.(*Response)
				method := c.fetchStratumMethod(resp.ID)
				if method == "" {
					log.Errorf("no request found for response with id: %d",
						resp.ID, spew.Sdump(resp))
					c.conn.Close()
					c.cancel()
					continue
				}

				log.Errorf("unknown request method for response: %s", method)
				c.conn.Close()
				c.cancel()
				continue

			default:
				log.Errorf("unknown message type received: %s", msgType)
				c.conn.Close()
				c.cancel()
				continue
			}
		}
	}
}

// handleAntminerDR3 prepares work notifications for the Antminer DR3.
func (c *Client) handleAntminerDR3Work(req *Request) {
	jobID, prevBlock, genTx1, genTx2, blockVersion, nBits, nTime,
		cleanJob, err := ParseWorkNotification(req)
	if err != nil {
		log.Errorf("unable to parse work message: %v", err)
	}

	// The DR3 requires the nBits and nTime fields of a mining.notify message
	// as big endian.
	nBits, err = hexReversed(nBits)
	if err != nil {
		log.Errorf("unable to hex reverse nBits: %v", err)
		c.cancel()
		return
	}

	nTime, err = hexReversed(nTime)
	if err != nil {
		log.Errorf("unable to hex reverse nTime: %v", err)
		c.cancel()
		return
	}

	prevBlockRev := reversePrevBlockWords(prevBlock)
	workNotif := WorkNotification(jobID, prevBlockRev,
		genTx1, genTx2, blockVersion, nBits, nTime, cleanJob)

	log.Tracef("DR3/DR5 work notification is: %v", spew.Sdump(workNotif))

	err = c.encoder.Encode(workNotif)
	if err != nil {
		log.Errorf("Message encoding error: %v", err)
		c.cancel()
		return
	}
}

// handleInnosiliconD9Work prepares work notifications for the Innosilicon D9.
func (c *Client) handleInnosiliconD9Work(req *Request) {
	jobID, prevBlock, genTx1, genTx2, blockVersion, nBits, nTime,
		cleanJob, err := ParseWorkNotification(req)
	if err != nil {
		log.Errorf("unable to parse work message: %v", err)
	}

	// The D9 requires the nBits and nTime fields of a mining.notify message
	// as big endian.
	nBits, err = hexReversed(nBits)
	if err != nil {
		log.Errorf("unable to hex reverse nBits: %v", err)
		c.cancel()
		return
	}

	nTime, err = hexReversed(nTime)
	if err != nil {
		log.Errorf("unable to hex reverse nTime: %v", err)
		c.cancel()
		return
	}

	prevBlockRev := reversePrevBlockWords(prevBlock)
	workNotif := WorkNotification(jobID, prevBlockRev,
		genTx1, genTx2, blockVersion, nBits, nTime, cleanJob)

	log.Tracef("D9 work notification is: %v", spew.Sdump(workNotif))

	err = c.encoder.Encode(workNotif)
	if err != nil {
		log.Errorf("message encoding error: %v", err)
		c.cancel()
		return
	}
}

// handleWhatsminerD1Work prepares work notifications for the Whatsminer D1.
func (c *Client) handleWhatsminerD1Work(req *Request) {
	jobID, prevBlock, genTx1, genTx2, blockVersion, nBits, nTime,
		cleanJob, err := ParseWorkNotification(req)
	if err != nil {
		log.Errorf("unable to parse work message: %v", err)
	}

	// The D1 requires the nBits and nTime fields of a mining.notify message
	// as little endian. Since they're already in the preferred format there
	// is no need to reverse bytes for nBits and nTime.

	prevBlockRev := reversePrevBlockWords(prevBlock)
	workNotif := WorkNotification(jobID, prevBlockRev,
		genTx1, genTx2, blockVersion, nBits, nTime, cleanJob)

	log.Tracef("D1 work notification is: %v", spew.Sdump(workNotif))

	err = c.encoder.Encode(workNotif)
	if err != nil {
		log.Errorf("message encoding error: %v", err)
		c.cancel()
		return
	}
}

// handleCPUWork prepares work for the cpu miner.
func (c *Client) handleCPUWork(req *Request) {
	err := c.encoder.Encode(req)
	if err != nil {
		log.Errorf("Message encoding error: %v", err)
		c.cancel()
		return
	}
}

func (c *Client) hashMonitor(ctx context.Context) {
	ticker := time.NewTicker(time.Second * hashCalcThreshold)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.wg.Done()
			return

		case <-ticker.C:
			submissions := atomic.LoadInt64(&c.submissions)
			if submissions == 0 {
				continue
			}

			average := float64(hashCalcThreshold / submissions)

			num := new(big.Rat).Mul(c.endpoint.diffData.difficulty,
				new(big.Rat).SetFloat64(c.endpoint.hub.cfg.NonceIterations))
			denom := new(big.Rat).SetFloat64(average)

			hash := new(big.Rat).Quo(num, denom)

			c.hashRateMtx.Lock()
			c.hashRate = new(big.Rat).Quo(new(big.Rat).Add(c.hashRate, hash),
				new(big.Rat).SetInt64(2))
			c.hashRateMtx.Unlock()

			atomic.StoreInt64(&c.submissions, 0)
		}
	}
}

// Send dispatches messages to a pool client. It must be run as a goroutine.
func (c *Client) send(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			c.wg.Done()
			return

		case msg := <-c.ch:
			if msg == nil {
				continue
			}

			log.Tracef("Message sent to %s is: %v", c.generateID(),
				spew.Sdump(msg))

			if msg.MessageType() == ResponseMessage {
				err := c.encoder.Encode(msg)
				if err != nil {
					log.Errorf("Message encoding error: %v", err)
					c.cancel()
					continue
				}
			}

			if msg.MessageType() == RequestMessage {
				req := msg.(*Request)
				if req.Method == Notify {
					id := c.generateID()

					// Only send work to authorized and subscribed clients.
					if !c.authorized || !c.subscribed {
						continue
					}

					switch c.endpoint.miner {
					case CPU:
						c.handleCPUWork(req)
						log.Tracef("%s notified of new work", id)

					case AntminerDR3, AntminerDR5:
						c.handleAntminerDR3Work(req)
						log.Tracef("%s notified of new work", id)

					case InnosiliconD9:
						c.handleInnosiliconD9Work(req)
						log.Tracef("%s notified of new work", id)

					case WhatsminerD1:
						c.handleWhatsminerD1Work(req)
						log.Tracef("%s notified of new work", id)

					default:
						log.Errorf("unknown miner provided to receive work: %s",
							c.endpoint.miner)
						c.cancel()
						continue
					}
				}

				if req.Method != Notify {
					err := c.encoder.Encode(msg)
					if err != nil {
						log.Errorf("message encoding error: %v", err)
						c.cancel()
						continue
					}
				}
			}
		}
	}
}

// run handles the process lifecycles of the pool client.
func (c *Client) run(ctx context.Context) {
	c.endpoint.wg.Add(1)
	go c.read()

	c.wg.Add(3)
	go c.process(ctx)
	go c.send(ctx)
	go c.hashMonitor(ctx)
	c.wg.Wait()

	c.shutdown()
	c.endpoint.wg.Done()
}
