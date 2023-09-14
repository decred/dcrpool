// Copyright (c) 2019-2023 The Decred developers
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
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"

	errs "github.com/decred/dcrpool/errors"
)

const (
	// maxMessageSize represents the maximum size of a transmitted message
	// allowed, in bytes.
	maxMessageSize = 250

	// hashCalcThreshold represents the minimum operating time before a
	// client's hash rate is calculated.
	hashCalcThreshold = time.Second * 20

	// rollWorkCycle represents the tick interval for asserting the need for
	// timestamp-rolled work.
	rollWorkCycle = time.Second
)

var (
	// ZeroInt is the default value for a big.Int.
	ZeroInt = new(big.Int).SetInt64(0)

	// ZeroRat is the default value for a big.Rat.
	ZeroRat = new(big.Rat).SetInt64(0)

	// zeroHash is the default value for a chainhash.Hash.
	zeroHash = chainhash.Hash{0}

	// mainNetName is the name of the main network.  It is stored as a variable
	// so only a single instance creation is needed.
	mainNetName = chaincfg.MainNetParams().Name
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
	// FetchMinerDifficulty returns the difficulty information for the
	// provided miner, if it exists.
	FetchMinerDifficulty func(string) (*DifficultyInfo, error)
	// Disconnect relays a disconnection signal to the client endpoint.
	Disconnect func()
	// RemoveClient removes the client from the pool.
	RemoveClient func(*Client)
	// SubmitWork sends solved block data to the consensus daemon.
	SubmitWork func(context.Context, *string) (bool, error)
	// FetchCurrentWork returns the current work of the pool.
	FetchCurrentWork func() string
	// WithinLimit returns if the client is still within its request limits.
	WithinLimit func(string, int) bool
	// HashCalcThreshold represents the minimum operating time before a
	// client's hash rate is calculated.
	HashCalcThreshold time.Duration
	// MaxGenTime represents the share creation target time for the pool.
	MaxGenTime time.Duration
	// ClientTimeout represents the connection read/write timeout.
	ClientTimeout time.Duration
	// SignalCache sends the provided cache update event to the gui cache.
	SignalCache func(event CacheUpdateEvent)
	// MonitorCycle represents the time monitoring a mining client to access
	// possible upgrades if needed.
	MonitorCycle time.Duration
	// MaxUpgradeTries represents the maximum number of consecutive miner
	// monitoring and upgrade tries.
	MaxUpgradeTries uint32
	// RollWorkCycle represents the tick interval for asserting the need for
	// timestamp-rolled work.
	RollWorkCycle time.Duration
}

// Client represents a client connection.
type Client struct {
	submissions  int64 // update atomically.
	lastWorkTime int64 // update atomically.

	// These fields track the miner identification and associated
	// difficulty info.
	miner    string
	id       string
	diffInfo *DifficultyInfo
	mtx      sync.RWMutex

	addr        *net.TCPAddr
	cfg         *ClientConfig
	conn        net.Conn
	encoder     *json.Encoder
	reader      *bufio.Reader
	ctx         context.Context
	cancel      context.CancelFunc
	name        string
	extraNonce1 string
	ch          chan Message
	readCh      chan readPayload
	account     string

	// These fields track the authorization and subscription status of
	// the miner.
	authorized bool
	subscribed bool
	statusMtx  sync.RWMutex

	hashRate    *big.Rat
	hashRateMtx sync.RWMutex
	wg          sync.WaitGroup
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

	return c, nil
}

// shutdown terminates all client processes and established connections.
func (c *Client) shutdown() {
	c.cfg.RemoveClient(c)

	c.mtx.RLock()
	id := c.id
	c.mtx.RUnlock()

	log.Tracef("%s connection terminated.", id)
}

// claimWeightedShare records a weighted share for the pool client. This
// serves as proof of verifiable work contributed to the mining pool.
func (c *Client) claimWeightedShare() error {
	if c.cfg.SoloPool {
		desc := "cannot claim shares in solo pool mode"
		return errs.PoolError(errs.ClaimShare, desc)
	}

	c.mtx.RLock()
	miner := c.miner
	c.mtx.RUnlock()

	if c.cfg.ActiveNet.Name == mainNetName && miner == CPU {
		desc := "cannot claim shares for cpu miners on mainnet, " +
			"reserved for testing purposes only (simnet, testnet)"
		return errs.PoolError(errs.ClaimShare, desc)
	}
	weight := ShareWeights[miner]
	share := NewShare(c.account, weight)
	return c.cfg.db.PersistShare(share)
}

// handleAuthorizeRequest processes authorize request messages received.
func (c *Client) handleAuthorizeRequest(req *Request, allowed bool) error {
	if !allowed {
		err := fmt.Errorf("unable to process authorize request, " +
			"client request limit reached")
		sErr := NewStratumError(Unknown, err)
		resp := AuthorizeResponse(*req.ID, false, sErr)
		c.ch <- resp
		return errs.PoolError(errs.LimitExceeded, err.Error())
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
			return errs.MsgError(errs.Parse, err.Error())
		}

		name := strings.TrimSpace(parts[1])
		address := strings.TrimSpace(parts[0])

		// Ensure the address is valid for the current network.
		_, err = stdaddr.DecodeAddress(address, c.cfg.ActiveNet)
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
			if !errors.Is(err, errs.ValueFound) {
				sErr := NewStratumError(Unknown, err)
				resp := AuthorizeResponse(*req.ID, false, sErr)
				c.ch <- resp
				return err
			}
		}

		c.account = account.UUID
		c.name = name

	case true:
		// Set a default account id.
		c.account = defaultAccountID

		c.name = username
	}

	c.statusMtx.Lock()
	c.authorized = true
	c.statusMtx.Unlock()
	resp := AuthorizeResponse(*req.ID, true, nil)
	c.ch <- resp

	return nil
}

// monitor periodically checks the miner details set against expected
// incoming submission tally and upgrades the miner if possible when the
// submission tallies exceed the expected number by 30 percent.
func (c *Client) monitor(idx int, pair *minerIDPair, monitorCycle time.Duration, maxTries uint32) {
	var subs, tries uint32
	if len(pair.miners) <= 1 {
		// Nothing to do if there are no more miner ids to upgrade to.
		return
	}

	expected := float64(monitorCycle / c.cfg.MaxGenTime)
	for {
		ticker := time.NewTicker(monitorCycle)
		defer ticker.Stop()

		select {
		case <-ticker.C:
			if idx == len(pair.miners)-1 {
				// No more miner upgrades possible.
				return
			}

			// Stop monitoring for possible upgrades when maxTries is reached.
			if tries == maxTries {
				return
			}

			subs = uint32(atomic.LoadInt64(&c.submissions)) - subs
			delta := float64(subs) - expected

			// Upgrade the miner only if there are 30 percent more
			// submissions than expected.
			if delta < 0.0 || delta < expected*0.3 {
				// Increment the number of tries on a failed upgrade attempt.
				tries++

				continue
			}

			idx++

			// Update the miner's details and send a new mining.set_difficulty
			// message to the client.
			c.mtx.Lock()
			miner := pair.miners[idx]
			newID := fmt.Sprintf("%v/%v", c.extraNonce1, miner)
			log.Infof("upgrading %s to %s", c.id, newID)
			info, err := c.cfg.FetchMinerDifficulty(miner)
			if err != nil {
				tries++
				log.Error(err)
				c.mtx.Unlock()
				continue
			}
			c.miner = miner
			c.id = newID
			c.diffInfo = info
			c.mtx.Unlock()

			c.setDifficulty()
			log.Infof("updated difficulty (%s) for %s sent",
				c.diffInfo.difficulty.FloatString(3), c.id)
			c.updateWork(true)

			// Reset tries after a successful upgrade.
			tries = 0

		case <-c.ctx.Done():
			return
		}
	}
}

// handleSubscribeRequest processes subscription request messages received.
func (c *Client) handleSubscribeRequest(req *Request, allowed bool) error {
	if !allowed {
		err := fmt.Errorf("unable to process subscribe request, client " +
			"request limit reached")
		sErr := NewStratumError(Unknown, err)
		resp := SubscribeResponse(*req.ID, "", "", 0, sErr)
		c.ch <- resp
		return errs.PoolError(errs.LimitExceeded, err.Error())
	}

	mid, nid, err := ParseSubscribeRequest(req)
	if err != nil {
		sErr := NewStratumError(Unknown, err)
		resp := SubscribeResponse(*req.ID, "", "", 0, sErr)
		c.ch <- resp
		return err
	}

	// Identify the miner and fetch needed mining information for it.
	idPair, err := identifyMiner(mid)
	if err != nil {
		sErr := NewStratumError(Unknown, err)
		resp := SubscribeResponse(*req.ID, "", "", 0, sErr)
		c.ch <- resp
		return errs.PoolError(errs.MinerUnknown, err.Error())
	}

	c.mtx.Lock()
	minerIdx := 0
	miner := idPair.miners[minerIdx]
	info, err := c.cfg.FetchMinerDifficulty(miner)
	if err != nil {
		c.mtx.Unlock()
		return err
	}
	c.miner = miner
	c.id = fmt.Sprintf("%v/%v", c.extraNonce1, miner)
	c.diffInfo = info
	c.mtx.Unlock()

	// Generate a subscription id if none exists.
	if nid == "" {
		nid = fmt.Sprintf("mn%v", c.extraNonce1)
	}

	go c.monitor(minerIdx, idPair, c.cfg.MonitorCycle, c.cfg.MaxUpgradeTries)

	var resp *Response
	switch miner {
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

	c.statusMtx.Lock()
	c.subscribed = true
	c.statusMtx.Unlock()

	c.ch <- resp

	return nil
}

// handleExtraNonceSubscribeRequest processes extranonce subscribe requests.
func (c *Client) handleExtraNonceSubscribeRequest(req *Request, allowed bool) error {
	if !allowed {
		err := fmt.Errorf("unable to process extranonce subscribe request," +
			"client request limit reached")
		sErr := NewStratumError(Unknown, err)
		resp := SubscribeResponse(*req.ID, "", "", 0, sErr)
		c.ch <- resp
		return errs.PoolError(errs.LimitExceeded, err.Error())
	}

	err := ParseExtraNonceSubscribeRequest(req)
	if err != nil {
		sErr := NewStratumError(Unknown, err)
		resp := SubscribeResponse(*req.ID, "", "", 0, sErr)
		c.ch <- resp
		return err
	}

	resp := ExtraNonceSubscribeResponse(*req.ID)
	c.ch <- resp

	return nil
}

// setDifficulty sends the pool client's difficulty ratio.
func (c *Client) setDifficulty() {
	// Do not send a difficulty notification if the diff info
	// for the miner is not set.
	if c.diffInfo == nil {
		return
	}

	c.mtx.RLock()
	diffRat := c.diffInfo.difficulty
	c.mtx.RUnlock()
	diff := new(big.Rat).Set(diffRat)
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
		return errs.PoolError(errs.LimitExceeded, err.Error())
	}

	c.mtx.RLock()
	id := c.id
	miner := c.miner
	powLimit := c.diffInfo.powLimit
	diff := c.diffInfo.difficulty
	tgt := c.diffInfo.target
	c.mtx.RUnlock()

	_, jobID, extraNonce2E, nTimeE, nonceE, err :=
		ParseSubmitWorkRequest(req, miner)
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
		extraNonce2E, nTimeE, nonceE, miner)
	if err != nil {
		sErr := NewStratumError(Unknown, err)
		resp := SubmitWorkResponse(*req.ID, false, sErr)
		c.ch <- resp
		return err
	}
	target := new(big.Rat).SetInt(standalone.CompactToBig(header.Bits))

	// The target difficulty must be larger than zero.
	if target.Sign() <= 0 {
		err := fmt.Errorf("block target difficulty of %064x is too "+
			"low", target)
		sErr := NewStratumError(Unknown, err)
		resp := SubmitWorkResponse(*req.ID, false, sErr)
		c.ch <- resp
		return errs.PoolError(errs.LowDifficulty, err.Error())
	}
	hash := header.BlockHash()
	hashTarget := new(big.Rat).SetInt(standalone.HashToBig(&hash))
	netDiff := new(big.Rat).Quo(powLimit, target)
	hashDiff := new(big.Rat).Quo(powLimit, hashTarget)
	log.Tracef("network difficulty is: %s", netDiff.FloatString(4))
	log.Tracef("pool difficulty is: %s", diff.FloatString(4))
	log.Tracef("hash difficulty is: %s", hashDiff.FloatString(4))

	// Only submit work to the network if the submitted blockhash is
	// less than the pool target for the client.
	if hashTarget.Cmp(tgt) > 0 {
		err := fmt.Errorf("submitted work %s from %s is not less than its "+
			"corresponding pool target", hash.String(), id)
		sErr := NewStratumError(LowDifficultyShare, err)
		resp := SubmitWorkResponse(*req.ID, false, sErr)
		c.ch <- resp
		return errs.PoolError(errs.PoolDifficulty, err.Error())
	}
	atomic.AddInt64(&c.submissions, 1)

	// Claim a weighted share for work contributed to the pool if not mining
	// in solo mining mode.
	if !c.cfg.SoloPool {
		err := c.claimWeightedShare()
		if err != nil {
			err := fmt.Errorf("%s: %w", id, err)
			sErr := NewStratumError(Unknown, err)
			resp := SubmitWorkResponse(*req.ID, false, sErr)
			c.ch <- resp
			return errs.PoolError(errs.ClaimShare, err.Error())
		}

		// Signal the gui cache of the claimed weighted share.
		c.cfg.SignalCache(ClaimedShare)
	}

	// Only submit work to the network if the submitted blockhash is
	// less than the network target difficulty.
	if hashTarget.Cmp(target) > 0 {
		// Accept the submitted work but note it is not less than the
		// network target difficulty.
		resp := SubmitWorkResponse(*req.ID, true, nil)
		c.ch <- resp

		desc := fmt.Sprintf("submitted work %s from %s is not "+
			"less than the network target difficulty", hash.String(), id)
		return errs.PoolError(errs.NetworkDifficulty, desc)
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
			id, hash.String())
		if err != nil {
			// send the current work if the error is a block difficulty mismatch.
			if strings.Contains(err.Error(), "block difficulty of") {
				c.updateWork(true)
			}

			desc = fmt.Sprintf("%s: work %s rejected by the network (%v)",
				id, hash.String(), err)
		}

		return errs.PoolError(errs.WorkRejected, desc)
	}

	// Create accepted work if the work submission is accepted
	// by the mining node.
	work := NewAcceptedWork(hash.String(), header.PrevBlock.String(),
		header.Height, c.account, miner)
	err = c.cfg.db.persistAcceptedWork(work)
	if err != nil {
		// If the submitted accepted work already exists, ignore the
		// submission.
		if errors.Is(err, errs.ValueFound) {
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
	ticker := time.NewTicker(c.cfg.RollWorkCycle)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
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
				c.updateWork(false)
			}
		}
	}
}

// read receives incoming data and passes the message received for
// processing. This must be run as goroutine.
func (c *Client) read() {
	for {
		c.mtx.RLock()
		id := c.id
		c.mtx.RUnlock()

		err := c.conn.SetDeadline(time.Now().Add(c.cfg.ClientTimeout))
		if err != nil {
			log.Errorf("%s: unable to set deadline: %v", id, err)
			c.cancel()
			return
		}
		data, err := c.reader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Errorf("%s: EOF", id)
				c.cancel()
				return
			}
			var nErr *net.OpError
			if !errors.As(err, &nErr) {
				log.Errorf("%s: unable to read bytes: %v", id, err)
				c.cancel()
				return
			}
			if nErr.Op == "read" && nErr.Net == "tcp" {
				switch {
				case nErr.Timeout():
					log.Errorf("%s: read timeout: %v", id, err)
				case !nErr.Timeout():
					log.Errorf("%s: read error: %v", id, err)
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
			log.Errorf("unable to identify message %q: %v", data, err)
			c.cancel()
			return
		}
		c.readCh <- readPayload{msg, reqType}
	}
}

// updateWork updates a client with a timestamp-rolled current work with the
// provided clean job status. This should be called after a client
// completes a work submission,after client authentication and when the
// client is stalling on current work.
func (c *Client) updateWork(cleanJob bool) {
	const funcName = "updateWork"
	// Only timestamp-roll current work for authorized and subscribed clients.
	c.statusMtx.RLock()
	authorized := c.authorized
	subscribed := c.subscribed
	c.statusMtx.RUnlock()

	c.mtx.RLock()
	id := c.id
	c.mtx.RUnlock()

	if !subscribed || !authorized {
		log.Debugf("can only timestamp-roll work for subscribed " +
			"and authorized clients")
		return
	}
	currWorkE := c.cfg.FetchCurrentWork()
	if currWorkE == "" {
		log.Tracef("no work available to send %s", id)
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
		blockVersion, nBits, nTime, cleanJob)
	select {
	case c.ch <- workNotif:
		log.Tracef("Sent a timestamp-rolled current work at "+
			"height #%v to %v", height, id)
	default:
		// Non-blocking send fallthrough.
	}
}

// process  handles incoming messages from the connected pool client.
// It must be run as a goroutine.
func (c *Client) process() {
	for {
		select {
		case <-c.ctx.Done():
			err := c.conn.Close()
			if err != nil {
				c.mtx.RLock()
				id := c.id
				c.mtx.RUnlock()
				log.Errorf("%s: unable to close connection: %v", id, err)
			}
			c.wg.Done()
			return

		case payload := <-c.readCh:
			msg := payload.msg
			msgType := payload.msgType
			allowed := c.cfg.WithinLimit(c.addr.String(), PoolClient)
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
						c.updateWork(true)
					}

				case Subscribe:
					err := c.handleSubscribeRequest(req, allowed)
					if err != nil {
						log.Error(err)
						continue
					}

				case ExtraNonceSubscribe:
					err := c.handleExtraNonceSubscribeRequest(req, allowed)
					if err != nil {
						log.Error(err)
						continue
					}

				case Submit:
					err := c.handleSubmitWorkRequest(c.ctx, req, allowed)
					if errors.Is(err, errs.NetworkDifficulty) {
						// Submissions less than the network difficulty should
						// not be treated as errors.
						log.Debug(err)
						continue
					}

					if err != nil {
						log.Error(err)
						continue
					}

				default:
					log.Errorf("unknown request method for message %s: %s",
						req.String(), req.Method)
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

				log.Errorf("unexpected response message received: %s", string(r))
				c.cancel()
				continue

			default:
				log.Errorf("unknown type received for message %s: %d",
					msg.String(), msgType)
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
		return "", errs.PoolError(errs.HexLength, desc)
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
		log.Errorf("%s: work encoding error: %v", miner, err)
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
	c.mtx.RLock()
	miner := c.miner
	c.mtx.RUnlock()
	return miner
}

// FetchAccountID gets the client's account ID.
func (c *Client) FetchAccountID() string {
	return c.account
}

// hashMonitor calculates the total number of hashes being solved by the
// client periodically.
func (c *Client) hashMonitor() {
	var subs, cycle int64
	iterations := new(big.Rat).SetFloat64(c.cfg.NonceIterations)
	hashCalcThresholdSecs := float64(c.cfg.HashCalcThreshold / time.Second)

	ticker := time.NewTicker(c.cfg.HashCalcThreshold)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			c.wg.Done()
			return

		case <-ticker.C:
			cycle++

			submissions := atomic.LoadInt64(&c.submissions)
			if submissions == 0 {
				continue
			}

			c.mtx.RLock()
			if c.diffInfo == nil {
				c.mtx.RUnlock()
				continue
			}

			diff := c.diffInfo.difficulty
			c.mtx.RUnlock()

			delta := submissions - subs

			if delta == 0 {
				continue
			}

			average := hashCalcThresholdSecs * float64(cycle) / float64(delta)
			num := new(big.Rat).Mul(diff, iterations)
			denom := new(big.Rat).SetFloat64(average)
			hash := new(big.Rat).Quo(num, denom)
			c.setHashRate(hash)
			subs = submissions
			cycle = 0

			c.mtx.RLock()
			miner := c.miner
			c.mtx.RUnlock()

			hashID := hashDataID(c.account, c.extraNonce1)
			hashData, err := c.cfg.db.fetchHashData(hashID)
			if err != nil {
				if errors.Is(err, errs.ValueNotFound) {
					hashData = newHashData(miner, c.account, c.addr.String(),
						c.extraNonce1, hash)
					err = c.cfg.db.persistHashData(hashData)
					if err != nil {
						log.Errorf("unable to persist hash data with "+
							"id %s: %v", hashData.UUID, err)
					}

					continue
				}

				log.Errorf("unable to fetch hash data with id %s: %v",
					hashID, err)

				c.cancel()
				continue
			}

			hashData.HashRate = hash
			hashData.UpdatedOn = time.Now().UnixNano()

			err = c.cfg.db.updateHashData(hashData)
			if err != nil {
				log.Errorf("unable to update hash data with "+
					"id %s: %v", hashData.UUID, err)
			}
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
					log.Errorf("encoding error for message %s: %v",
						msg.String(), err)
					c.cancel()
					continue
				}
			}

			if msg.MessageType() == RequestMessage {
				req := msg.(*Request)
				if req.Method == Notify {
					// Only send work to authorized and subscribed clients.
					c.statusMtx.Lock()
					authorized := c.authorized
					subscribed := c.subscribed
					c.statusMtx.Unlock()
					if !authorized || !subscribed {
						continue
					}

					c.mtx.RLock()
					miner := c.miner
					id := c.id
					c.mtx.RUnlock()

					switch miner {
					case CPU, NiceHashValidator:
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

					case ObeliskDCR1:
						c.handleObeliskDCR1Work(req)
						log.Tracef("%s notified of new work", id)

					default:
						log.Errorf("unknown miner for client: %s, "+
							"message: %s", miner, req.String())
						c.cancel()
						continue
					}
				}
				if req.Method != Notify {
					err := c.encoder.Encode(msg)
					if err != nil {
						log.Errorf("encoding error for message %s: %v",
							msg.String(), err)
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
	go c.read()

	c.wg.Add(4)
	go c.process()
	go c.send()
	go c.hashMonitor()
	go c.rollWork()
	c.wg.Wait()

	c.shutdown()
	c.cfg.Disconnect()
}
