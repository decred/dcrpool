// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/wire"

	"github.com/decred/dcrd/blockchain"

	"github.com/davecgh/go-spew/spew"
	"github.com/decred/dcrd/dcrutil"
	"github.com/dnldd/dcrpool/database"
	"github.com/dnldd/dcrpool/dividend"
)

// Client represents a client connection.
type Client struct {
	conn        net.Conn
	endpoint    *Endpoint
	encoder     *json.Encoder
	reader      *bufio.Reader
	ctx         context.Context
	cancel      context.CancelFunc
	ip          string
	id          uint64
	extraNonce1 string
	ch          chan Message
	req         map[uint64]string
	reqMtx      sync.RWMutex
	account     string
	authorized  bool
	subscribed  bool
}

// NewClient creates client connection instance.
func NewClient(conn net.Conn, endpoint *Endpoint, ip string) *Client {
	ctx, cancel := context.WithCancel(context.TODO())
	endpoint.hub.AddClient(endpoint.miner)
	return &Client{
		conn:     conn,
		endpoint: endpoint,
		ctx:      ctx,
		cancel:   cancel,
		ch:       make(chan Message),
		encoder:  json.NewEncoder(conn),
		reader:   bufio.NewReader(conn),
		ip:       ip,
	}
}

// recordRequest logs a request as an id/method pair.
func (c *Client) recordRequest(id uint64, method string) {
	c.reqMtx.Lock()
	c.req[id] = method
	c.reqMtx.Unlock()
}

// fetchRequest fetches the method of the recorded request id.
func (c *Client) fetchRequest(id uint64) string {
	c.reqMtx.RLock()
	method := c.req[id]
	c.reqMtx.RUnlock()
	return method
}

// deleteRequest removes the recorded request referenced by the provided id.
func (c *Client) deleteRequest(id uint64) {
	c.reqMtx.Lock()
	delete(c.req, id)
	c.reqMtx.Unlock()
}

// nextID returns the next message id for the client.
func (c *Client) nextID() *uint64 {
	id := atomic.AddUint64(&c.id, 1)
	return &id
}

// Shutdown terminates all client processes and established connections.
func (c *Client) Shutdown() {
	c.endpoint.hub.RemoveClient(c.endpoint.miner)
	close(c.ch)
	err := c.conn.Close()
	if err != nil {
		log.Errorf("Client connection close error: %v", err)
	}
}

// claimWeightedShare records a weighted share for the pool client. This serves
// as proof of verifiable work contributed to the mining pool.
func (c *Client) claimWeightedShare() {
	weight := dividend.ShareWeights[c.endpoint.miner]
	share := dividend.NewShare(c.account, weight)
	share.Create(c.endpoint.hub.db)
	log.Tracef("Weighted share claimed")
}

// handleAuthorizeRequest processes authorize request messages received.
func (c *Client) handleAuthorizeRequest(req *Request, allowed bool) {
	if !allowed {
		err := NewStratumError(Unknown)
		resp := AuthorizeResponse(req.ID, false, err)
		c.ch <- resp
		return
	}

	// Usernames are expected to be of `address:id` format.
	username, err := ParseAuthorizeRequest(req)
	if err != nil {
		log.Errorf("Failed to parse authorize request: %v", err)
		err := NewStratumError(Unknown)
		resp := AuthorizeResponse(req.ID, false, err)
		c.ch <- resp
		return
	}

	parts := strings.Split(username, ".")
	if len(parts) != 2 {
		log.Errorf("Invalid username format, expected `address.id`,got %v",
			username)
		err := NewStratumError(Unknown)
		resp := AuthorizeResponse(req.ID, false, err)
		c.ch <- resp
		return
	}

	name := strings.TrimSpace(parts[1])
	address := strings.TrimSpace(parts[0])

	// Ensure the provided address is valid and associated with the active
	// network.
	addr, err := dcrutil.DecodeAddress(address)
	if err != nil {
		log.Errorf("Failed to decode address: %v", err)
		err := NewStratumError(Unknown)
		resp := AuthorizeResponse(req.ID, false, err)
		c.ch <- resp
		return
	}

	if !addr.IsForNet(c.endpoint.hub.cfg.ActiveNet) {
		log.Errorf("Address (%v) is not associated with the active network"+
			" (%v)", address, c.endpoint.hub.cfg.ActiveNet.Name)
		err := NewStratumError(Unknown)
		resp := AuthorizeResponse(req.ID, false, err)
		c.ch <- resp
		return
	}

	id := dividend.AccountID(name, address)
	_, err = dividend.FetchAccount(c.endpoint.hub.db, []byte(*id))
	if err != nil && err.Error() !=
		database.ErrValueNotFound([]byte(*id)).Error() {
		log.Errorf("Failed to fetch account: %v", err)
		err := NewStratumError(Unknown)
		resp := AuthorizeResponse(req.ID, false, err)
		c.ch <- resp
		return
	}

	// Create the account if it does not already exist.
	account, err := dividend.NewAccount(name, address)
	if err != nil {
		log.Errorf("Failed to create account: %v", err)
		err := NewStratumError(Unknown)
		resp := AuthorizeResponse(req.ID, false, err)
		c.ch <- resp
		return
	}

	err = account.Create(c.endpoint.hub.db)
	if err != nil {
		log.Errorf("Failed to persist account: %v", err)
		err := NewStratumError(Unknown)
		resp := AuthorizeResponse(req.ID, false, err)
		c.ch <- resp
		return
	}

	c.authorized = true
	c.account = *id
	resp := AuthorizeResponse(req.ID, true, nil)
	c.ch <- resp
}

// handleSubscribeRequest processes subscription request messages received.
func (c *Client) handleSubscribeRequest(req *Request, allowed bool) {
	if !allowed {
		err := NewStratumError(Unknown)
		resp := SubscribeResponse(req.ID, "", "", err)
		c.ch <- resp
		return
	}

	_, nid, err := ParseSubscribeRequest(req)
	if err != nil {
		log.Errorf("Failed to parse subscribe request: %v", err)
		err := NewStratumError(Unknown)
		resp := SubscribeResponse(req.ID, "", "", err)
		c.ch <- resp
		return
	}

	c.extraNonce1 = GenerateExtraNonce1()
	if nid == "" {
		nid = fmt.Sprintf("mn%v", c.extraNonce1)
	}

	resp := SubscribeResponse(req.ID, nid, c.extraNonce1, nil)
	log.Tracef("Subscribe response is: %v", spew.Sdump(resp))

	c.ch <- resp
	c.subscribed = true
}

// setDifficulty sends the pool client's difficulty ratio.
func (c *Client) setDifficulty() {
	log.Tracef("Difficulty is %v", c.endpoint.diffData.difficulty)
	diffNotif := SetDifficultyNotification(
		c.endpoint.diffData.difficulty)
	c.ch <- diffNotif
}

// handleSubmitWorkRequest processes work submission request messages received.
func (c *Client) handleSubmitWorkRequest(req *Request, allowed bool) {
	if !allowed {
		err := NewStratumError(Unknown)
		resp := SubmitWorkResponse(req.ID, false, err)
		c.ch <- resp
		return
	}

	log.Tracef("Received work submission is: %v", spew.Sdump(req))

	_, jobID, extraNonce2E, nTimeE, nonceE, err := ParseSubmitWorkRequest(req,
		c.endpoint.miner)
	if err != nil {
		log.Errorf("Failed to parse submit work request: %v", err)
		err := NewStratumError(Unknown)
		resp := SubmitWorkResponse(req.ID, false, err)
		c.ch <- resp
		return
	}

	job, err := FetchJob(c.endpoint.hub.db, []byte(jobID))
	if err != nil {
		log.Errorf("Failed to fetch job: %v", err)
		err := NewStratumError(Unknown)
		resp := SubmitWorkResponse(req.ID, false, err)
		c.ch <- resp
		return
	}

	header, nonceSpaceE, err := GenerateSolvedBlockHeader(job.Header,
		c.extraNonce1, extraNonce2E, nTimeE, nonceE, c.endpoint.miner)
	if err != nil {
		log.Errorf("Failed to generate solved block header: %v", err)
		err := NewStratumError(Unknown)
		resp := SubmitWorkResponse(req.ID, false, err)
		c.ch <- resp
		return
	}

	log.Tracef("solved block header is: %v", spew.Sdump(header))

	poolTarget := c.endpoint.diffData.target
	target := blockchain.CompactToBig(header.Bits)
	hash := header.BlockHash()
	hashNum := blockchain.HashToBig(&hash)

	log.Tracef("pool target is: %v", poolTarget)
	log.Tracef("hash target is: %v", hashNum)

	// Only submit work to the network if the submitted blockhash is
	// below the target difficulty and the specified pool target
	// for the client.
	if hashNum.Cmp(poolTarget) > 0 {
		log.Errorf("submitted work is not less than the "+
			"client's (%v) pool target", c.endpoint.miner)
		err := NewStratumError(LowDifficultyShare)
		resp := SubmitWorkResponse(req.ID, false, err)
		c.ch <- resp
		return
	}

	// Claim a weighted share for work contributed to the pool.
	c.claimWeightedShare()

	// Send an accepted work response to the pool client.
	c.ch <- SubmitWorkResponse(req.ID, true, nil)

	if hashNum.Cmp(target) < 0 {
		// Persist the accepted work, this is a workaround for having
		// an accepted work record available when a block connected
		// notification is received.

		work := NewAcceptedWork(nonceSpaceE)
		work.Create(c.endpoint.hub.db)

		// Generate and send the work submission.
		headerB, err := header.Bytes()
		if err != nil {
			log.Errorf("Failed to fetch block header bytes: %v", err)
			err := NewStratumError(Unknown)
			resp := SubmitWorkResponse(req.ID, false, err)
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
			log.Errorf("Failed to submit work request: %v", err)
			err := NewStratumError(Unknown)
			resp := SubmitWorkResponse(req.ID, false, err)
			c.ch <- resp
			return
		}

		log.Tracef("Work accepted status is: %v", accepted)

		if !accepted {
			work.Delete(c.endpoint.hub.db)
		}
	}
}

// Listen reads and processes incoming messages from the connected pool client.
func (c *Client) Listen() {
	//TODO: work in a read timeout and max message size for the tcp connection.
	for {
		select {
		case <-c.ctx.Done():
			return

		default:
			// Non-blocking receive fallthrough.
		}

		c.conn.SetReadDeadline(time.Now().Add(time.Minute * 3))
		c.conn.SetWriteDeadline(time.Now().Add(time.Minute * 3))
		data, err := c.reader.ReadBytes('\n')
		if err != nil {
			log.Errorf("Failed to read bytes: %v", err)
			c.cancel()
			continue
		}

		log.Tracef("Message received is %v", string(data))

		msg, reqType, err := IdentifyMessage(data)
		if err != nil {
			log.Errorf("Failed to identify message: %v", err)
			c.cancel()
			continue
		}

		// Ensure the requesting client is within their request limits.
		allowed := c.endpoint.hub.limiter.WithinLimit(c.ip)

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
				log.Errorf("Unknown request method for request: %s", req.Method)
			}

		case ResponseType:
			resp := msg.(*Response)
			method := c.fetchRequest(*resp.ID)
			if method == "" {
				log.Errorf("No request found for response with id: ", resp.ID,
					spew.Sdump(resp))
				c.cancel()
				continue
			}

			switch method {
			default:
				log.Errorf("Unknown request method for response: %s", method)
			}

		default:
			log.Errorf("Unknown message type received: %s", reqType)
		}
	}
}

// ReversePrevBlockWords reverses each 4-byte word in the provided hex encoded
// previous block hash.
func ReversePrevBlockWords(hashE string) string {
	buf := bytes.NewBufferString("")
	for i := 0; i < len(hashE); i += 8 {
		buf.WriteString(hashE[i+6 : i+8])
		buf.WriteString(hashE[i+4 : i+6])
		buf.WriteString(hashE[i+2 : i+4])
		buf.WriteString(hashE[i : i+2])
	}
	return buf.String()
}

// HexReversed reverses a hex string.
func HexReversed(in string) (string, error) {
	if len(in)%2 != 0 {
		return "", fmt.Errorf("incorrect hex input length")
	}

	buf := bytes.NewBufferString("")
	for i := len(in) - 1; i > -1; i -= 2 {
		buf.WriteByte(in[i-1])
		buf.WriteByte(in[i])
	}
	return buf.String(), nil
}

// handleAntminerDR3 prepares work notifications for the Antminer DR3.
func (c *Client) handleAntminerDR3Work(req *Request) {
	jobID, prevBlock, genTx1, genTx2, blockVersion, nBits, nTime,
		cleanJob, err := ParseWorkNotification(req)
	if err != nil {
		log.Errorf("Failed to parse work message: %v", err)
	}

	// The DR3 requires the nBits and nTime fields of a mining.notify message
	// as big endian.
	nBits, err = HexReversed(nBits)
	if err != nil {
		log.Errorf("Failed to hex reverse nBits: %v", err)
		c.cancel()
		return
	}

	nTime, err = HexReversed(nTime)
	if err != nil {
		log.Errorf("Failed to hex reverse nTime: %v", err)
		c.cancel()
		return
	}

	prevBlockRev := ReversePrevBlockWords(prevBlock)
	workNotif := WorkNotification(jobID, prevBlockRev,
		genTx1, genTx2, blockVersion, nBits, nTime, cleanJob)

	log.Tracef("DR3 work notification is: %v", spew.Sdump(workNotif))

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
		log.Errorf("Failed to parse work message: %v", err)
	}

	// The D9 requires the nBits and nTime fields of a mining.notify message
	// as big endian.
	nBits, err = HexReversed(nBits)
	if err != nil {
		log.Errorf("Failed to hex reverse nBits: %v", err)
		c.cancel()
		return
	}

	nTime, err = HexReversed(nTime)
	if err != nil {
		log.Errorf("Failed to hex reverse nTime: %v", err)
		c.cancel()
		return
	}

	prevBlockRev := ReversePrevBlockWords(prevBlock)
	workNotif := WorkNotification(jobID, prevBlockRev,
		genTx1, genTx2, blockVersion, nBits, nTime, cleanJob)

	log.Tracef("D9 work notification is: %v", spew.Sdump(workNotif))

	err = c.encoder.Encode(workNotif)
	if err != nil {
		log.Errorf("Message encoding error: %v", err)
		c.cancel()
		return
	}
}

// Send dispatches messages to a pool client. It must be run as a goroutine.
func (c *Client) Send() {
	for {
		select {
		case <-c.ctx.Done():
			c.Shutdown()
			return

		case msg := <-c.ch:
			if msg == nil {
				continue
			}

			err := c.encoder.Encode(msg)
			if err != nil {
				log.Errorf("Message encoding error: %v", err)
				c.cancel()
				continue
			}

		case msg := <-c.endpoint.hub.Broadcast:
			if msg == nil {
				continue
			}

			req := msg.(*Request)
			switch req.Method {
			case Notify:
				switch c.endpoint.miner {
				case dividend.CPU:
					err := c.encoder.Encode(msg)
					if err != nil {
						log.Errorf("Message encoding error: %v", err)
						c.cancel()
						continue
					}

				case dividend.AntminerDR3, dividend.AntminerDR5:
					c.handleAntminerDR3Work(req)

				case dividend.InnosiliconD9:
					c.handleInnosiliconD9Work(req)

				default:
					log.Errorf("Unknown miner provided to receive work: %v",
						c.endpoint.miner)
					c.cancel()
					continue
				}
			}
		}
	}
}
