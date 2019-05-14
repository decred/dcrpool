// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrpool/database"
	"github.com/decred/dcrpool/dividend"
	"github.com/decred/dcrpool/util"
)

const (
	// MaxMessageSize represents the maximum size of a transmitted message
	// allowed, in bytes.
	MaxMessageSize = 250
)

// Client represents a client connection.
type Client struct {
	conn               net.Conn
	endpoint           *Endpoint
	encoder            *json.Encoder
	reader             *bufio.Reader
	ctx                context.Context
	cancel             context.CancelFunc
	ip                 string
	name               string
	extraNonce1        string
	ch                 chan Message
	readCh             chan []byte
	req                map[uint64]string
	reqMtx             sync.RWMutex
	account            string
	authorized         bool
	subscribed         bool
	lastSubmissionTime *big.Int
	hashRate           *big.Rat
	hashRateMtx        sync.RWMutex
	wg                 sync.WaitGroup
}

// NewClient creates client connection instance.
func NewClient(conn net.Conn, endpoint *Endpoint, ip string) *Client {
	ctx, cancel := context.WithCancel(context.TODO())
	c := &Client{
		conn:               conn,
		endpoint:           endpoint,
		ctx:                ctx,
		cancel:             cancel,
		ch:                 make(chan Message),
		readCh:             make(chan []byte),
		encoder:            json.NewEncoder(conn),
		reader:             bufio.NewReaderSize(conn, MaxMessageSize),
		ip:                 ip,
		lastSubmissionTime: util.ZeroInt,
		hashRate:           util.ZeroRat,
	}

	c.GenerateExtraNonce1()

	return c
}

// generateID creates a unique id of for the pool client.
func (c *Client) generateID() string {
	return fmt.Sprintf("%v/%v", c.extraNonce1, c.endpoint.miner)
}

// GenerateExtraNonce1 generates a random 4-byte extraNonce1 for the
// client.
func (c *Client) GenerateExtraNonce1() {
	id := make([]byte, 4)
	rand.Read(id)
	c.extraNonce1 = hex.EncodeToString(id)
}

// fetchRequest fetches the method of the recorded request id.
func (c *Client) fetchRequest(id uint64) string {
	c.reqMtx.RLock()
	method := c.req[id]
	c.reqMtx.RUnlock()
	return method
}

// shutdown terminates all client processes and established connections.
func (c *Client) shutdown() {
	c.conn.Close()
	close(c.ch)
	close(c.readCh)
	c.endpoint.hub.limiter.RemoveLimiter(c.ip)
	c.endpoint.RemoveClient(c)
	log.Tracef("Connection to (%v) terminated.", c.generateID())
}

// calculateHashRate generates the client hash rate based on work submissions
// from the miner.
func (c *Client) calculateHashRate() error {
	now := new(big.Int).SetInt64(time.Now().Unix())
	if c.lastSubmissionTime.Cmp(util.ZeroInt) == 0 {
		c.lastSubmissionTime = now
		return nil
	}

	expectedShareTime := new(big.Int).Add(c.lastSubmissionTime,
		c.endpoint.hub.cfg.MaxGenTime)
	diff := new(big.Rat).SetFrac(now, expectedShareTime)
	hash := dividend.MinerHashes[c.endpoint.miner]
	if hash == nil {
		return fmt.Errorf("miner hash not found for key (%s)",
			c.endpoint.miner)
	}

	c.lastSubmissionTime = now

	c.hashRateMtx.Lock()
	c.hashRate = new(big.Rat).Mul(diff, new(big.Rat).SetInt(hash))
	c.hashRateMtx.Unlock()

	return nil
}

// claimWeightedShare records a weighted share for the pool client. This serves
// as proof of verifiable work contributed to the mining pool. This also
// updates the hash rate of the miner based on the last submitted share time.
func (c *Client) claimWeightedShare() error {
	if c.endpoint.hub.cfg.ActiveNet.Name == chaincfg.MainNetParams.Name &&
		c.endpoint.miner == dividend.CPU {
		log.Error("CPU miners are reserved for only simnet testing purposes")
		return nil
	}

	weight := dividend.ShareWeights[c.endpoint.miner]
	share := dividend.NewShare(c.account, weight)
	err := share.Create(c.endpoint.hub.db)
	if err != nil {
		return err
	}

	log.Tracef("Weighted share of (%v) for pool client (%v) claimed",
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

	// The client's username is expected to be of the format address.clientname
	// when in pool mining mode. For solo pool mode the username expected is
	// just the client's name.
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
			log.Errorf("Invalid username format, expected `address.id`,got %v",
				username)
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
			log.Errorf("Address (%v) is not associated with the active network"+
				" (%v)", address, c.endpoint.hub.cfg.ActiveNet.Name)
			err := NewStratumError(Unknown, nil)
			resp := AuthorizeResponse(*req.ID, false, err)
			c.ch <- resp
			return
		}

		id := dividend.AccountID(address)
		_, err = dividend.FetchAccount(c.endpoint.hub.db, []byte(*id))
		if err != nil && err.Error() !=
			database.ErrValueNotFound([]byte(*id)).Error() {
			log.Errorf("unable to fetch account: %v", err)
			err := NewStratumError(Unknown, nil)
			resp := AuthorizeResponse(*req.ID, false, err)
			c.ch <- resp
			return
		}

		// Create the account if it does not already exist.
		account, err := dividend.NewAccount(address)
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

		c.account = *id
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
		resp := SubscribeResponse(*req.ID, "", "", err)
		c.ch <- resp
		return
	}

	_, nid, err := ParseSubscribeRequest(req)
	if err != nil {
		log.Errorf("unable to parse subscribe request: %v", err)
		err := NewStratumError(Unknown, nil)
		resp := SubscribeResponse(*req.ID, "", "", err)
		c.ch <- resp
		return
	}

	if nid == "" {
		nid = fmt.Sprintf("mn%v", c.extraNonce1)
	}

	resp := SubscribeResponse(*req.ID, nid, c.extraNonce1, nil)
	log.Tracef("Subscribe response is: %v", spew.Sdump(resp))

	c.ch <- resp
	c.subscribed = true
}

// setDifficulty sends the pool client's difficulty ratio.
func (c *Client) setDifficulty() {
	log.Tracef("Difficulty is %v", c.endpoint.diffData.difficulty)
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

	log.Tracef("Received work submission from (%v) is %v",
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

	log.Tracef("Submitted work from (%v) is %v", c.generateID(),
		spew.Sdump(header))
	log.Infof("Submited work hash at height (%v) is (%v)", header.Height,
		header.BlockHash().String())

	poolTarget := c.endpoint.diffData.target
	target := blockchain.CompactToBig(header.Bits)
	hash := header.BlockHash()
	hashNum := blockchain.HashToBig(&hash)

	log.Tracef("pool target is: %v", poolTarget)
	log.Tracef("hash target is: %v", hashNum)

	// Only submit work to the network if the submitted blockhash is
	// below the pool target for the client.
	if hashNum.Cmp(poolTarget) > 0 {
		log.Errorf("submitted work from (%v) is not less than its"+
			" corresponding pool target", c.generateID())
		err := NewStratumError(LowDifficultyShare, nil)
		resp := SubmitWorkResponse(*req.ID, false, err)
		c.ch <- resp
		return
	}

	// Calculate the hash rate of the client.
	err = c.calculateHashRate()
	if err != nil {
		log.Errorf("unable to calculate hash rate of (%v): %v",
			c.generateID(), err)
	}

	// Claim a weighted share for work contributed to the pool if not mining
	// in solo mining mode.
	if !c.endpoint.hub.cfg.SoloPool {
		err := c.claimWeightedShare()
		if err != nil {
			log.Errorf("failed to persist weighted share for (%v): %v",
				c.generateID(), err)
			err := NewStratumError(Unknown, nil)
			resp := SubmitWorkResponse(*req.ID, false, err)
			c.ch <- resp
			return
		}
	}

	// Only submit work to the network if the submitted blockhash is
	// below the network target difficulty.
	if hashNum.Cmp(target) > 0 {
		log.Tracef("submitted work from (%v) is not less than the"+
			" network target difficulty", c.generateID())
		resp := SubmitWorkResponse(*req.ID, true, nil)
		c.ch <- resp
		return
	}

	if hashNum.Cmp(target) < 0 {
		// Persist the accepted work before submiting to the network. This is
		// a workaround in order to have an accepted work record available
		// when a block connected notification is received.

		work := NewAcceptedWork(hash.String(), header.PrevBlock.String(),
			header.Height, c.account, c.endpoint.miner)
		err := work.Create(c.endpoint.hub.db)
		if err != nil {
			// If the submitted accetped work already exists, ignore the submission.
			if err.Error() == ErrWorkAlreadyExists([]byte(work.UUID)).Error() {
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

		log.Tracef("Work accepted status is: %v", accepted)
		c.ch <- SubmitWorkResponse(*req.ID, accepted, nil)

		// Remove the work record if it is not accepted by the network.
		if !accepted {
			work.Delete(c.endpoint.hub.db)
		}
	}
}

// read receives incoming data and passes the message received for
// processing. This must be run as goroutine.
func (c *Client) read() {
	c.conn.SetReadDeadline(time.Now().Add(time.Minute * 3))
	c.conn.SetWriteDeadline(time.Now().Add(time.Minute * 3))

	for {
		data, err := c.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return
			}

			if nErr := err.(*net.OpError); nErr != nil {
				if nErr.Op == "read" && nErr.Net == "tcp" {
					c.cancel()
					return
				}
			}

			log.Errorf("failed to read bytes: %v %T", err, err)
			c.cancel()
			return
		}

		log.Tracef("Message received from (%v) is %v", c.generateID(),
			spew.Sdump(data))

		c.readCh <- data
	}
}

// process  handles incoming messages from the connected pool client.
// It must be run as a goroutine.
func (c *Client) process(ctx context.Context) {
	log.Tracef("Listener for (%v) started.", c.generateID())

	for {
		select {
		case <-ctx.Done():
			log.Tracef("Listener for (%v) done.", c.generateID())
			c.wg.Done()
			return

		case data := <-c.readCh:
			msg, reqType, err := IdentifyMessage(data)
			if err != nil {
				log.Errorf("unable to identify message: %v", err)
				c.cancel()
				continue
			}

			// Ensure the requesting client is within their request limits.
			allowed := c.endpoint.hub.limiter.WithinLimit(c.ip, PoolClient)

			switch reqType {
			case RequestType:
				req := msg.(*Request)
				switch req.Method {
				case Authorize:
					c.handleAuthorizeRequest(req, allowed)
					c.setDifficulty()

				case Subscribe:
					c.handleSubscribeRequest(req, allowed)

				case Submit:
					c.handleSubmitWorkRequest(req, allowed)

				default:
					log.Errorf("unknown request method for request: %s", req.Method)
				}

			case ResponseType:
				resp := msg.(*Response)
				method := c.fetchRequest(resp.ID)
				if method == "" {
					log.Errorf("no request found for response with id: ", resp.ID,
						spew.Sdump(resp))
					c.cancel()
					continue
				}

				log.Errorf("unknown request method for response: %s", method)

			default:
				log.Errorf("unknown message type received: %s", reqType)
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
	nBits, err = util.HexReversed(nBits)
	if err != nil {
		log.Errorf("unable to hex reverse nBits: %v", err)
		c.cancel()
		return
	}

	nTime, err = util.HexReversed(nTime)
	if err != nil {
		log.Errorf("unable to hex reverse nTime: %v", err)
		c.cancel()
		return
	}

	prevBlockRev := util.ReversePrevBlockWords(prevBlock)
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
	nBits, err = util.HexReversed(nBits)
	if err != nil {
		log.Errorf("unable to hex reverse nBits: %v", err)
		c.cancel()
		return
	}

	nTime, err = util.HexReversed(nTime)
	if err != nil {
		log.Errorf("unable to hex reverse nTime: %v", err)
		c.cancel()
		return
	}

	prevBlockRev := util.ReversePrevBlockWords(prevBlock)
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

	prevBlockRev := util.ReversePrevBlockWords(prevBlock)
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

// Send dispatches messages to a pool client. It must be run as a goroutine.
func (c *Client) send(ctx context.Context) {
	log.Tracef("Send handler for (%v) started.", c.generateID())

	for {
		select {
		case <-ctx.Done():
			log.Tracef("Send handler for (%v) done.", c.generateID())
			c.wg.Done()
			return

		case msg := <-c.ch:
			if msg == nil {
				continue
			}

			log.Tracef("Message sent to (%v) is %v", c.generateID(),
				spew.Sdump(msg))

			if msg.MessageType() == ResponseType {
				err := c.encoder.Encode(msg)
				if err != nil {
					log.Errorf("Message encoding error: %v", err)
					c.cancel()
					continue
				}
			}

			if msg.MessageType() == RequestType {
				req := msg.(*Request)
				if req.Method == Notify {
					id := c.generateID()

					switch c.endpoint.miner {
					case dividend.CPU:
						err := c.encoder.Encode(msg)
						if err != nil {
							log.Errorf("Message encoding error: %v", err)
							c.cancel()
							continue
						}

						log.Tracef("Client (%v) notified of new work", id)

					case dividend.AntminerDR3, dividend.AntminerDR5:
						c.handleAntminerDR3Work(req)
						log.Tracef("Client (%v) notified of new work", id)

					case dividend.InnosiliconD9:
						c.handleInnosiliconD9Work(req)
						log.Tracef("Client (%v) notified of new work", id)

					case dividend.WhatsminerD1:
						c.handleWhatsminerD1Work(req)
						log.Tracef("Client (%v) notified of new work", id)

					default:
						log.Errorf("unknown miner provided to receive work: %v",
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

	c.wg.Add(2)
	go c.process(ctx)
	go c.send(ctx)
	c.wg.Wait()

	c.shutdown()
	c.endpoint.wg.Done()
}
