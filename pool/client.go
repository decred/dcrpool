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
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/wire"
)

const (
	// maxMessageSize represents the maximum size of a transmitted message
	// allowed, in bytes.
	maxMessageSize = 250

	// hashCalcThreshold represents the minimum operating time in seconds
	// before a client's hash rate is calculated.
	hashCalcThreshold = 20

	// clientTimeout represents the read/write timeout for the client.
	clientTimeout = time.Minute * 4
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

// ClientConfig contains all of the configuration values which should be
// provided when creating a new instance of Client.
type ClientConfig struct {
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
	// Miner returns the endpoint miner type.
	FetchMiner func() string
	// DifficultyInfo represents the difficulty info for the client.
	DifficultyInfo *DifficultyInfo
	// EndpointWg is the waitgroup of the client's endpoint.
	EndpointWg *sync.WaitGroup
	// RemoveClient removes the client from the pool.
	RemoveClient func(*Client)
	// SubmitWork sends solved block data to the consensus daemon.
	SubmitWork func(context.Context, *string) (bool, error)
	// FetchCurrentWork returns the current work of the pool.
	FetchCurrentWork func() string
	// WithinLimit returns if the client is still within its request limits.
	WithinLimit func(string, int) bool
	// HashCalcThreshold represents the minimum operating time in seconds
	// before a client's hash rate is calculated.
	HashCalcThreshold uint32
	// MaxGenTime represents the share creation target time for the pool.
	MaxGenTime time.Duration
	// ClientTimeout represents the connection read/write timeout.
	ClientTimeout time.Duration
	// SignalCache sends the provided cache update event to the gui cache.
	SignalCache func(event CacheUpdateEvent)
}

// Client represents a client connection.
type Client struct {
	submissions  int64 // update atomically.
	lastWorkTime int64 // update atomically.

	id            string
	addr          *net.TCPAddr
	cfg           *ClientConfig
	conn          net.Conn
	encoder       *json.Encoder
	reader        *bufio.Reader
	ctx           context.Context
	cancel        context.CancelFunc
	name          string
	extraNonce1   string
	ch            chan Message
	readCh        chan readPayload
	account       string
	authorized    bool
	authorizedMtx sync.Mutex
	subscribed    bool
	subscribedMtx sync.Mutex
	hashRate      *big.Rat
	hashRateMtx   sync.RWMutex
	wg            sync.WaitGroup
}

// generateExtraNonce1 generates a random 4-byte extraNonce1
// for the client.
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
func NewClient(ctx context.Context, conn net.Conn, addr *net.TCPAddr, cCfg *ClientConfig) (*Client, error) {
	ctx, cancel := context.WithCancel(ctx)
	c := &Client{
		addr:     addr,
		cfg:      cCfg,
		conn:     conn,
		ctx:      ctx,
		cancel:   cancel,
		ch:       make(chan Message),
		readCh:   make(chan readPayload),
		encoder:  json.NewEncoder(conn),
		reader:   bufio.NewReaderSize(conn, maxMessageSize),
		hashRate: ZeroRat,
	}
	err := c.generateExtraNonce1()
	if err != nil {
		return nil, err
	}
	c.id = fmt.Sprintf("%v/%v", c.extraNonce1, c.cfg.FetchMiner())
	return c, nil
}

// shutdown terminates all client processes and established connections.
func (c *Client) shutdown() {
	c.cfg.RemoveClient(c)
	log.Tracef("%s connection terminated.", c.id)
}

// claimWeightedShare records a weighted share for the pool client. This
// serves as proof of verifiable work contributed to the mining pool.
func (c *Client) claimWeightedShare() error {
	if c.cfg.SoloPool {
		desc := "cannot claim shares in solo pool mode"
		return poolError(ErrClaimShare, desc)
	}
	if c.cfg.ActiveNet.Name == chaincfg.MainNetParams().Name &&
		c.cfg.FetchMiner() == CPU {
		desc := "cannot claim shares for cpu miners on mainnet, " +
			"reserved for testing purposes only (simnet, testnet)"
		return poolError(ErrClaimShare, desc)
	}
	weight := ShareWeights[c.cfg.FetchMiner()]
	share := NewShare(c.account, weight)
	return c.cfg.db.persistShare(share)
}

// handleAuthorizeRequest processes authorize request messages received.
func (c *Client) handleAuthorizeRequest(req *Request, allowed bool) error {
	if !allowed {
		err := fmt.Errorf("unable to process authorize request, " +
			"client request limit reached")
		sErr := NewStratumError(Unknown, err)
		resp := AuthorizeResponse(*req.ID, false, sErr)
		c.ch <- resp
		return poolError(ErrLimitExceeded, err.Error())
	}

	// The client's username is expected to be of the format address.clientid
	// when in pool mining mode. For solo pool mode the username expected is
	// just the client's id.
	username, err := ParseAuthorizeRequest(req)
	if err != nil {
		sErr := NewStratumError(Unknown, err)
		resp := AuthorizeResponse(*req.ID, false, sErr)
		c.ch <- resp
		return err
	}

	switch c.cfg.SoloPool {
	case false:
		parts := strings.Split(username, ".")
		if len(parts) != 2 {
			err := fmt.Errorf("invalid username format, expected "+
				"`address.clientid`, got %v", username)
			sErr := NewStratumError(Unknown, err)
			resp := AuthorizeResponse(*req.ID, false, sErr)
			c.ch <- resp
			return msgError(ErrParse, err.Error())
		}

		name := strings.TrimSpace(parts[1])
		address := strings.TrimSpace(parts[0])

		// Ensure the address is valid for the current network.
		_, err = dcrutil.DecodeAddress(address, c.cfg.ActiveNet)
		if err != nil {
			sErr := NewStratumError(Unknown, err)
			resp := AuthorizeResponse(*req.ID, false, sErr)
			c.ch <- resp
			return err
		}

		// Create the account if it does not already exist.
		account := NewAccount(address)
		err = c.cfg.db.persistAccount(account)
		if err != nil {
			// Do not error if the account already exists.
			if !errors.Is(err, ErrValueFound) {
				sErr := NewStratumError(Unknown, err)
				resp := AuthorizeResponse(*req.ID, false, sErr)
				c.ch <- resp
				return err
			}
		}

		c.account = account.UUID
		c.name = name

	case true:
		c.name = username
	}

	c.authorizedMtx.Lock()
	c.authorized = true
	c.authorizedMtx.Unlock()
	resp := AuthorizeResponse(*req.ID, true, nil)
	c.ch <- resp

	return nil
}

// handleSubscribeRequest processes subscription request messages received.
func (c *Client) handleSubscribeRequest(req *Request, allowed bool) error {
	if !allowed {
		err := fmt.Errorf("unable to process subscribe request, client " +
			"request limit reached")
		sErr := NewStratumError(Unknown, err)
		resp := SubscribeResponse(*req.ID, "", "", 0, sErr)
		c.ch <- resp
		return poolError(ErrLimitExceeded, err.Error())
	}

	_, nid, err := ParseSubscribeRequest(req)
	if err != nil {
		sErr := NewStratumError(Unknown, err)
		resp := SubscribeResponse(*req.ID, "", "", 0, sErr)
		c.ch <- resp
		return err
	}

	// Generate a subscription id if none exists.
	if nid == "" {
		nid = fmt.Sprintf("mn%v", c.extraNonce1)
	}

	var resp *Response
	switch c.cfg.FetchMiner() {
	case ObeliskDCR1:
		// The DCR1 is not fully complaint with the stratum spec.
		// It uses a 4-byte extraNonce2 regardless of the
		// extraNonce2Size provided.
		resp = SubscribeResponse(*req.ID, nid, c.extraNonce1,
			ExtraNonce2Size, nil)

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

	default:
		// The default case handles mining clients that support the
		// stratum spec and respect the extraNonce2Size provided.
		resp = SubscribeResponse(*req.ID, nid, c.extraNonce1, ExtraNonce2Size, nil)
	}

	c.subscribedMtx.Lock()
	c.subscribed = true
	c.subscribedMtx.Unlock()

	c.ch <- resp

	return nil
}

// setDifficulty sends the pool client's difficulty ratio.
func (c *Client) setDifficulty() {
	diff := new(big.Rat).Set(c.cfg.DifficultyInfo.difficulty)
	diffNotif := SetDifficultyNotification(diff)
	c.ch <- diffNotif
}

// handleSubmitWorkRequest processes work submission request messages received.
func (c *Client) handleSubmitWorkRequest(ctx context.Context, req *Request, allowed bool) error {
	if !allowed {
		err := fmt.Errorf("unable to process submit work request, client " +
			"request limit reached")
		sErr := NewStratumError(Unknown, err)
		resp := SubmitWorkResponse(*req.ID, false, sErr)
		c.ch <- resp
		return poolError(ErrLimitExceeded, err.Error())
	}

	_, jobID, extraNonce2E, nTimeE, nonceE, err :=
		ParseSubmitWorkRequest(req, c.cfg.FetchMiner())
	if err != nil {
		sErr := NewStratumError(Unknown, err)
		resp := SubmitWorkResponse(*req.ID, false, sErr)
		c.ch <- resp
		return err
	}
	job, err := c.cfg.db.fetchJob(jobID)
	if err != nil {
		sErr := NewStratumError(Unknown, err)
		resp := SubmitWorkResponse(*req.ID, false, sErr)
		c.ch <- resp
		return err
	}
	header, err := GenerateSolvedBlockHeader(job.Header, c.extraNonce1,
		extraNonce2E, nTimeE, nonceE, c.cfg.FetchMiner())
	if err != nil {
		sErr := NewStratumError(Unknown, err)
		resp := SubmitWorkResponse(*req.ID, false, sErr)
		c.ch <- resp
		return err
	}
	diffInfo := c.cfg.DifficultyInfo
	target := new(big.Rat).SetInt(standalone.CompactToBig(header.Bits))

	// The target difficulty must be larger than zero.
	if target.Sign() <= 0 {
		err := fmt.Errorf("block target difficulty of %064x is too "+
			"low", target)
		sErr := NewStratumError(Unknown, err)
		resp := SubmitWorkResponse(*req.ID, false, sErr)
		c.ch <- resp
		return poolError(ErrDifficulty, err.Error())
	}
	hash := header.BlockHash()
	hashTarget := new(big.Rat).SetInt(standalone.HashToBig(&hash))
	netDiff := new(big.Rat).Quo(diffInfo.powLimit, target)
	hashDiff := new(big.Rat).Quo(diffInfo.powLimit, hashTarget)
	log.Tracef("network difficulty is: %s", netDiff.FloatString(4))
	log.Tracef("pool difficulty is: %s", diffInfo.difficulty.FloatString(4))
	log.Tracef("hash difficulty is: %s", hashDiff.FloatString(4))

	// Only submit work to the network if the submitted blockhash is
	// less than the pool target for the client.
	if hashTarget.Cmp(diffInfo.target) > 0 {
		err := fmt.Errorf("submitted work from %s is not less than its "+
			"corresponding pool target", c.id)
		sErr := NewStratumError(LowDifficultyShare, err)
		resp := SubmitWorkResponse(*req.ID, false, sErr)
		c.ch <- resp
		return poolError(ErrDifficulty, err.Error())
	}
	atomic.AddInt64(&c.submissions, 1)

	// Claim a weighted share for work contributed to the pool if not mining
	// in solo mining mode.
	if !c.cfg.SoloPool {
		err := c.claimWeightedShare()
		if err != nil {
			err := fmt.Errorf("%s: %v", c.id, err)
			sErr := NewStratumError(Unknown, err)
			resp := SubmitWorkResponse(*req.ID, false, sErr)
			c.ch <- resp
			return poolError(ErrClaimShare, err.Error())
		}

		// Signal the gui cache of the claimed weighted share.
		c.cfg.SignalCache(ClaimedShare)
	}

	// Only submit work to the network if the submitted blockhash is
	// less than the network target difficulty.
	if hashTarget.Cmp(target) > 0 {
		resp := SubmitWorkResponse(*req.ID, false, nil)
		c.ch <- resp
		desc := fmt.Sprintf("submitted work from %s is not "+
			"less than the network target difficulty", c.id)
		return poolError(ErrDifficulty, desc)
	}

	// Generate and send the work submission.
	headerB, err := header.Bytes()
	if err != nil {
		sErr := NewStratumError(Unknown, err)
		resp := SubmitWorkResponse(*req.ID, false, sErr)
		c.ch <- resp
		return err
	}
	submissionB := make([]byte, getworkDataLen)
	copy(submissionB[:wire.MaxBlockHeaderPayload], headerB)
	copy(submissionB[wire.MaxBlockHeaderPayload:],
		c.cfg.Blake256Pad)
	submission := hex.EncodeToString(submissionB)
	accepted, err := c.cfg.SubmitWork(ctx, &submission)
	if err != nil {
		sErr := NewStratumError(Unknown, err)
		resp := SubmitWorkResponse(*req.ID, false, sErr)
		c.ch <- resp
		return err
	}

	if !accepted {
		c.ch <- SubmitWorkResponse(*req.ID, false, nil)
		desc := fmt.Sprintf("%s: work %s rejected by the network",
			c.id, hash.String())
		return poolError(ErrWorkRejected, desc)
	}

	// Create accepted work if the work submission is accepted
	// by the mining node.
	work := NewAcceptedWork(hash.String(), header.PrevBlock.String(),
		header.Height, c.account, c.cfg.FetchMiner())
	err = c.cfg.db.persistAcceptedWork(work)
	if err != nil {
		// If the submitted accepted work already exists, ignore the
		// submission.
		if errors.Is(err, ErrValueFound) {
			sErr := NewStratumError(DuplicateShare, err)
			resp := SubmitWorkResponse(*req.ID, false, sErr)
			c.ch <- resp
			return err
		}
		sErr := NewStratumError(Unknown, err)
		resp := SubmitWorkResponse(*req.ID, false, sErr)
		c.ch <- resp
		return err
	}
	log.Tracef("Work %s accepted by the network", hash.String())
	resp := SubmitWorkResponse(*req.ID, true, nil)
	c.ch <- resp
	return nil
}

// rollWork provides the client with timestamp-rolled work to avoid stalling.
func (c *Client) rollWork() {
	ticker := time.NewTicker(time.Second)

	for {
		select {
		case <-c.ctx.Done():
			ticker.Stop()
			c.wg.Done()
			return

		case <-ticker.C:
			// Send a timetamp-rolled work to the client if it fails to
			// generate a work submission in twice the time it is estimated
			// to according to its pool target.
			lastWorkTime := atomic.LoadInt64(&c.lastWorkTime)
			if lastWorkTime == 0 {
				continue
			}

			now := time.Now()
			if now.Sub(time.Unix(lastWorkTime, 0)) >= c.cfg.MaxGenTime*2 {
				c.updateWork()
			}
		}
	}
}

// read receives incoming data and passes the message received for
// processing. This must be run as goroutine.
func (c *Client) read() {
	for {
		err := c.conn.SetDeadline(time.Now().Add(c.cfg.ClientTimeout))
		if err != nil {
			log.Errorf("%s: unable to set deadline: %v", c.id, err)
			c.cancel()
			return
		}
		data, err := c.reader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Errorf("%s: EOF", c.id)
				c.cancel()
				return
			}
			var nErr *net.OpError
			if !errors.As(err, &nErr) {
				log.Errorf("%s: unable to read bytes: %v", c.id, err)
				c.cancel()
				return
			}
			if nErr.Op == "read" && nErr.Net == "tcp" {
				switch {
				case nErr.Timeout():
					log.Errorf("%s: read timeout: %v", c.id, err)
				case !nErr.Timeout():
					log.Errorf("%s: read error: %v", c.id, err)
				}
				c.cancel()
				return
			}
			log.Errorf("unable to read bytes: %v %T", err, err)
			c.cancel()
			return
		}
		msg, reqType, err := IdentifyMessage(data)
		if err != nil {
			log.Errorf("unable to identify message: %v", err)
			c.cancel()
			return
		}
		c.readCh <- readPayload{msg, reqType}
	}
}

// updateWork updates a client with a timestamp-rolled current work.
// This should be called after a client completes a work submission,
// after client authentication and when the client is stalling on
// current work.
func (c *Client) updateWork() {
	const funcName = "updateWork"
	// Only timestamp-roll current work for authorized and subscribed clients.
	c.authorizedMtx.Lock()
	authorized := c.authorized
	c.authorizedMtx.Unlock()
	c.subscribedMtx.Lock()
	subscribed := c.subscribed
	c.subscribedMtx.Unlock()

	if !subscribed || !authorized {
		return
	}
	currWorkE := c.cfg.FetchCurrentWork()
	if currWorkE == "" {
		return
	}

	now := uint32(time.Now().Unix())
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, now)
	timestampE := hex.EncodeToString(b)
	var buf bytes.Buffer
	_, _ = buf.WriteString(currWorkE[:272])
	_, _ = buf.WriteString(timestampE)
	_, _ = buf.WriteString(currWorkE[280:])

	updatedWorkE := buf.String()
	blockVersion := updatedWorkE[:8]
	prevBlock := updatedWorkE[8:72]
	genTx1 := updatedWorkE[72:288]
	nBits := updatedWorkE[232:240]
	nTime := updatedWorkE[272:280]
	genTx2 := updatedWorkE[352:360]

	heightD, err := hex.DecodeString(updatedWorkE[256:264])
	if err != nil {
		log.Errorf("%s: unable to decode block height %s: %v", funcName,
			string(heightD), err)
		return
	}
	height := binary.LittleEndian.Uint32(heightD)

	// Create a job for the timestamp-rolled current work.
	job := NewJob(updatedWorkE, height)
	err = c.cfg.db.persistJob(job)
	if err != nil {
		log.Error(err)
		return
	}
	workNotif := WorkNotification(job.UUID, prevBlock, genTx1, genTx2,
		blockVersion, nBits, nTime, true)
	select {
	case c.ch <- workNotif:
		log.Tracef("Sent a timestamp-rolled current work at "+
			"height #%v to %v", height, c.id)
	default:
	}
}

// process  handles incoming messages from the connected pool client.
// It must be run as a goroutine.
func (c *Client) process() {
	ip := c.addr.String()
	for {
		select {
		case <-c.ctx.Done():
			_, err := c.conn.Write([]byte{})
			if err != nil {
				log.Errorf("%s: unable to send close message: %v", c.id, err)
			}
			c.wg.Done()
			return

		case payload := <-c.readCh:
			msg := payload.msg
			msgType := payload.msgType
			allowed := c.cfg.WithinLimit(ip, PoolClient)
			switch msgType {
			case RequestMessage:
				req := msg.(*Request)
				switch req.Method {
				case Authorize:
					err := c.handleAuthorizeRequest(req, allowed)
					if err != nil {
						log.Error(err)
						continue
					}
					if allowed {
						c.setDifficulty()
						time.Sleep(time.Second)
						c.updateWork()
					}

				case Subscribe:
					err := c.handleSubscribeRequest(req, allowed)
					if err != nil {
						log.Error(err)
						continue
					}

				case Submit:
					err := c.handleSubmitWorkRequest(c.ctx, req, allowed)
					if err != nil {
						log.Error(err)
						continue
					}
					if allowed {
						c.updateWork()
					}

				default:
					log.Errorf("unknown request method: %s", req.Method)
					c.cancel()
					continue
				}

			case ResponseMessage:
				resp := msg.(*Response)
				r, err := json.Marshal(resp)
				if err != nil {
					log.Errorf("unable to encode response as JSON: %v", err)
					c.cancel()
					continue
				}

				log.Errorf("unexpected response message received: %v", string(r))
				c.cancel()
				continue

			default:
				log.Errorf("unknown message type received: %d", msgType)
				c.cancel()
				continue
			}
		}
	}
}

// reversePrevBlockWords reverses each 4-byte word in the provided hex encoded
// previous block hash.
func reversePrevBlockWords(hashE string) string {
	var buf bytes.Buffer
	for i := 0; i < len(hashE); i += 8 {
		_, _ = buf.WriteString(hashE[i+6 : i+8])
		_, _ = buf.WriteString(hashE[i+4 : i+6])
		_, _ = buf.WriteString(hashE[i+2 : i+4])
		_, _ = buf.WriteString(hashE[i : i+2])
	}
	return buf.String()
}

// hexReversed reverses a hex string.
func hexReversed(in string) (string, error) {
	const funcName = "hexReversed"
	if len(in)%2 != 0 {
		desc := fmt.Sprintf("%s: expected even hex input length, got %d",
			funcName, len(in))
		return "", poolError(ErrHexLength, desc)
	}
	var buf bytes.Buffer
	for i := len(in) - 1; i > -1; i -= 2 {
		_ = buf.WriteByte(in[i-1])
		_ = buf.WriteByte(in[i])
	}
	return buf.String(), nil
}

// handleAntminerDR3 prepares work notifications for the Antminer DR3.
func (c *Client) handleAntminerDR3Work(req *Request) {
	miner := "AntminerDR3"
	jobID, prevBlock, genTx1, genTx2, blockVersion, nBits, nTime,
		cleanJob, err := ParseWorkNotification(req)
	if err != nil {
		log.Errorf("%s: %v", miner, err)
	}

	// The DR3 requires the nBits and nTime fields of a mining.notify message
	// as big endian.
	nBits, err = hexReversed(nBits)
	if err != nil {
		log.Errorf("%s: %v for nBits", miner, err)
		c.cancel()
		return
	}
	nTime, err = hexReversed(nTime)
	if err != nil {
		log.Errorf("%s: %v for nTime", miner, err)
		c.cancel()
		return
	}
	prevBlockRev := reversePrevBlockWords(prevBlock)
	workNotif := WorkNotification(jobID, prevBlockRev,
		genTx1, genTx2, blockVersion, nBits, nTime, cleanJob)
	err = c.encoder.Encode(workNotif)
	if err != nil {
		log.Errorf("%s: work encoding error, %v", miner, err)
		c.cancel()
		return
	}

	atomic.StoreInt64(&c.lastWorkTime, time.Now().Unix())
}

// handleInnosiliconD9Work prepares work notifications for the Innosilicon D9.
func (c *Client) handleInnosiliconD9Work(req *Request) {
	miner := "InnosiliconD9"
	jobID, prevBlock, genTx1, genTx2, blockVersion, nBits, nTime,
		cleanJob, err := ParseWorkNotification(req)
	if err != nil {
		log.Errorf("%s: %v", miner, err)
	}

	// The D9 requires the nBits and nTime fields of a mining.notify message
	// as big endian.
	nBits, err = hexReversed(nBits)
	if err != nil {
		log.Errorf("%s: %v for nBits", miner, err)
		c.cancel()
		return
	}
	nTime, err = hexReversed(nTime)
	if err != nil {
		log.Errorf("%s: %v for nTime", miner, err)
		c.cancel()
		return
	}
	prevBlockRev := reversePrevBlockWords(prevBlock)
	workNotif := WorkNotification(jobID, prevBlockRev,
		genTx1, genTx2, blockVersion, nBits, nTime, cleanJob)
	err = c.encoder.Encode(workNotif)
	if err != nil {
		log.Errorf("%s: work encoding error, %v", miner, err)
		c.cancel()
		return
	}

	atomic.StoreInt64(&c.lastWorkTime, time.Now().Unix())
}

// handleWhatsminerD1Work prepares work notifications for the Whatsminer D1.
func (c *Client) handleWhatsminerD1Work(req *Request) {
	miner := "WhatsminerD1"
	jobID, prevBlock, genTx1, genTx2, blockVersion, nBits, nTime,
		cleanJob, err := ParseWorkNotification(req)
	if err != nil {
		log.Errorf("%s: %v", miner, err)
	}

	// The D1 requires the nBits and nTime fields of a mining.notify message
	// as little endian. Since they're already in the preferred format there
	// is no need to reverse bytes for nBits and nTime.
	prevBlockRev := reversePrevBlockWords(prevBlock)
	workNotif := WorkNotification(jobID, prevBlockRev,
		genTx1, genTx2, blockVersion, nBits, nTime, cleanJob)
	err = c.encoder.Encode(workNotif)
	if err != nil {
		log.Errorf("%s: work encoding error: %v", miner, err)
		c.cancel()
		return
	}

	atomic.StoreInt64(&c.lastWorkTime, time.Now().Unix())
}

// handleCPUWork prepares work for the cpu miner.
func (c *Client) handleCPUWork(req *Request) {
	miner := "CPU"
	err := c.encoder.Encode(req)
	if err != nil {
		log.Errorf("%s: work encoding error, %v", miner, err)
		c.cancel()
		return
	}

	atomic.StoreInt64(&c.lastWorkTime, time.Now().Unix())
}

// handleObeliskDCR1Work prepares work for the Obelisk DCR1.
func (c *Client) handleObeliskDCR1Work(req *Request) {
	miner := "ObeliskDCR1"
	jobID, prevBlock, genTx1, genTx2, blockVersion, nBits, nTime,
		cleanJob, err := ParseWorkNotification(req)
	if err != nil {
		log.Errorf("%s: %v", miner, err)
	}

	// The DCR1 requires the nBits and nTime fields of a mining.notify message
	// as big endian.
	nBits, err = hexReversed(nBits)
	if err != nil {
		log.Errorf("%s: %v for nBits", miner, err)
		c.cancel()
		return
	}
	nTime, err = hexReversed(nTime)
	if err != nil {
		log.Errorf("%s: %v for nTime", miner, err)
		c.cancel()
		return
	}

	prevBlockRev := reversePrevBlockWords(prevBlock)
	workNotif := WorkNotification(jobID, prevBlockRev,
		genTx1, genTx2, blockVersion, nBits, nTime, cleanJob)
	err = c.encoder.Encode(workNotif)
	if err != nil {
		log.Errorf("%s: work encoding error, %v", miner, err)
		c.cancel()
		return
	}

	atomic.StoreInt64(&c.lastWorkTime, time.Now().Unix())
}

// setHashRate updates the client's hash rate.
func (c *Client) setHashRate(hash *big.Rat) {
	c.hashRateMtx.Lock()
	c.hashRate = new(big.Rat).Quo(new(big.Rat).Add(c.hashRate, hash),
		new(big.Rat).SetInt64(2))
	c.hashRateMtx.Unlock()
}

// FetchHashRate gets the client's hash rate.
func (c *Client) FetchHashRate() *big.Rat {
	c.hashRateMtx.Lock()
	defer c.hashRateMtx.Unlock()
	return c.hashRate
}

// FetchIPAddr gets the client's IP address.
func (c *Client) FetchIPAddr() string {
	return c.addr.String()
}

// FetchMinerType gets the client's miner type.
func (c *Client) FetchMinerType() string {
	return c.cfg.FetchMiner()
}

// FetchAccountID gets the client's account ID.
func (c *Client) FetchAccountID() string {
	return c.account
}

// hashMonitor calculates the total number of hashes being solved by the
// client periodically.
func (c *Client) hashMonitor() {
	ticker := time.NewTicker(time.Second * time.Duration(c.cfg.HashCalcThreshold))
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			c.wg.Done()
			return

		case <-ticker.C:
			submissions := atomic.LoadInt64(&c.submissions)
			if submissions == 0 {
				continue
			}
			average := float64(c.cfg.HashCalcThreshold) / float64(submissions)
			diffInfo := c.cfg.DifficultyInfo
			num := new(big.Rat).Mul(diffInfo.difficulty,
				new(big.Rat).SetFloat64(c.cfg.NonceIterations))
			denom := new(big.Rat).SetFloat64(average)
			hash := new(big.Rat).Quo(num, denom)
			c.setHashRate(hash)
			atomic.StoreInt64(&c.submissions, 0)
		}
	}
}

// Send dispatches messages to a pool client. It must be run as a goroutine.
func (c *Client) send() {
	for {
		select {
		case <-c.ctx.Done():
			c.wg.Done()
			return

		case msg := <-c.ch:
			if msg == nil {
				continue
			}
			if msg.MessageType() == ResponseMessage {
				err := c.encoder.Encode(msg)
				if err != nil {
					log.Errorf("message encoding error: %v", err)
					c.cancel()
					continue
				}
			}

			if msg.MessageType() == RequestMessage {
				req := msg.(*Request)
				if req.Method == Notify {
					// Only send work to authorized and subscribed clients.
					c.authorizedMtx.Lock()
					authorized := c.authorized
					c.authorizedMtx.Unlock()
					c.subscribedMtx.Lock()
					subscribed := c.subscribed
					c.subscribedMtx.Unlock()
					if !authorized || !subscribed {
						continue
					}

					switch c.cfg.FetchMiner() {
					case CPU:
						c.handleCPUWork(req)
						log.Tracef("%s notified of new work", c.id)

					case AntminerDR3, AntminerDR5:
						c.handleAntminerDR3Work(req)
						log.Tracef("%s notified of new work", c.id)

					case InnosiliconD9:
						c.handleInnosiliconD9Work(req)
						log.Tracef("%s notified of new work", c.id)

					case WhatsminerD1:
						c.handleWhatsminerD1Work(req)
						log.Tracef("%s notified of new work", c.id)

					case ObeliskDCR1:
						c.handleObeliskDCR1Work(req)
						log.Tracef("%s notified of new work", c.id)

					default:
						log.Errorf("unknown miner provided: %s",
							c.cfg.FetchMiner())
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
func (c *Client) run() {
	endpointWg := c.cfg.EndpointWg
	endpointWg.Add(1)
	go c.read()

	c.wg.Add(4)
	go c.process()
	go c.send()
	go c.hashMonitor()
	go c.rollWork()
	c.wg.Wait()

	c.shutdown()
	endpointWg.Done()
}
