// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/davecgh/go-spew/spew"

	"github.com/dnldd/dcrpool/dividend"
	"github.com/dnldd/dcrpool/network"
)

var (
	// statum defines the stratum protocol identifier.
	stratum = "stratum+tcp"
)

// Work represents the data recieved from a work notification. It comprises of
// hex encoded block header and pool target data.
type Work struct {
	jobID  string
	header []byte
	target *big.Int
}

// Miner represents a stratum mining client.
type Miner struct {
	conn            net.Conn
	core            *CPUMiner
	encoder         *json.Encoder
	reader          *bufio.Reader
	work            *Work
	workMtx         sync.RWMutex
	config          *config
	id              uint64
	req             map[uint64]string
	reqMtx          sync.RWMutex
	chainCh         chan struct{}
	authorized      bool
	subscribed      bool
	started         int64
	ctx             context.Context
	cancel          context.CancelFunc
	extraNonce1     string
	extraNonce2Size uint64
}

// recordRequest logs a request as an id/method pair.
func (m *Miner) recordRequest(id uint64, method string) {
	m.reqMtx.Lock()
	m.req[id] = method
	m.reqMtx.Unlock()
}

// fetchRequest fetches the method of the recorded request id.
func (m *Miner) fetchRequest(id uint64) string {
	m.reqMtx.RLock()
	method := m.req[id]
	m.reqMtx.RUnlock()
	return method
}

// deleteRequest removes the recorded request referenced by the provided id.
func (m *Miner) deleteRequest(id uint64) {
	m.reqMtx.Lock()
	delete(m.req, id)
	m.reqMtx.Unlock()
}

// nextID returns the next message id for the client.
func (m *Miner) nextID() *uint64 {
	id := atomic.AddUint64(&m.id, 1)
	return &id
}

// authenticate sends a stratum miner authentication message.
func (m *Miner) authenticate() error {
	id := m.nextID()
	req := network.AuthorizeRequest(id, m.config.User, m.config.Address)
	err := m.encoder.Encode(req)
	if err != nil {
		return err
	}

	m.recordRequest(*id, req.Method)

	return nil
}

// subscribe sends a stratum miner subscribe message.
func (m *Miner) subscribe() error {
	id := m.nextID()
	req := network.SubscribeRequest(id)
	err := m.encoder.Encode(req)
	if err != nil {
		return err
	}

	m.recordRequest(*id, req.Method)

	return nil
}

// NewMiner creates a stratum mining client.
func NewMiner(cfg *config) (*Miner, error) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Miner{
		config:  cfg,
		work:    new(Work),
		ctx:     ctx,
		cancel:  cancel,
		chainCh: make(chan struct{}),
		req:     make(map[uint64]string),
	}

	poolAddr := strings.Replace(m.config.Pool, stratum, "", 1)
	log.Tracef("Pool address is: %v", poolAddr)

	conn, err := net.Dial(network.TCP, poolAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s, %v", poolAddr, err)
	}

	m.conn = conn
	m.encoder = json.NewEncoder(m.conn)
	m.reader = bufio.NewReader(m.conn)

	err = m.subscribe()
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe miner: %v", err)
	}

	err = m.authenticate()
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate miner: %v", err)
	}

	m.started = time.Now().Unix()
	m.core = NewCPUMiner(m)
	m.core.Start()

	return m, nil
}

// Reconnect reestablishes a broken stratum connection.
func (m *Miner) Reconnect() error {
	poolAddr := strings.Replace(m.config.Pool, stratum, "", 1)
	conn, err := net.Dial(network.TCP, poolAddr)
	if err != nil {
		return fmt.Errorf("failed to reconnect to %s, %v", poolAddr, err)
	}

	m.conn = conn
	m.encoder = json.NewEncoder(m.conn)
	m.reader = bufio.NewReader(m.conn)

	err = m.subscribe()
	if err != nil {
		return fmt.Errorf("failed to subscribe miner: %v", err)
	}

	err = m.authenticate()
	if err != nil {
		return fmt.Errorf("fvailed to authenticate miner: %v", err)
	}

	m.started = time.Now().Unix()
	return nil
}

// Listen reads and processes incoming messages from the pool client. It must
// be run as a goroutine.
func (m *Miner) Listen() {
	log.Info("Miner listener started.")

out:
	for {
		select {
		case <-m.ctx.Done():
			close(m.chainCh)
			log.Info("Miner listener done.")
			break out

		default:
			// Non-blocking receive fallthrough.
		}

		//TODO: work in a read timeout and max bytes for the tcp connection.

		data, err := m.reader.ReadBytes('\n')
		if err != nil {
			continue
		}

		log.Tracef("Message received is: %v", spew.Sdump(string(data)))

		msg, reqType, err := network.IdentifyMessage(data)
		if err != nil {
			log.Errorf("Message identification error: %v", err)
			m.cancel()
			continue
		}

		switch reqType {
		case network.RequestType:
			req := msg.(*network.Request)
			switch req.Method {
			// TODO: Process requests from the mining pool.
			}

		case network.ResponseType:
			resp := msg.(*network.Response)
			method := m.fetchRequest(*resp.ID)
			if method == "" {
				log.Error("No request found for response with id: ", resp.ID,
					spew.Sdump(resp))
				m.cancel()
				continue
			}

			switch method {
			case network.Authorize:
				status, errStr, err := network.ParseAuthorizeResponse(resp)
				if err != nil {
					log.Errorf("Parse authorize response error: %v", err)
					m.cancel()
					continue
				}

				if errStr != nil {
					log.Errorf("Authorize error: %s", errStr)
					m.cancel()
					continue
				}

				if !status {
					log.Error("Authorize request for miner failed")
					m.cancel()
					continue
				}

				m.authorized = true
				log.Trace("Miner successfully authorized")

			case network.Subscribe:
				diffID, notifyID, extraNonce1E, extraNonce2Size, err :=
					network.ParseSubscribeResponse(resp)
				if err != nil {
					log.Errorf("Parse subscribe response error: %v", err)
					m.cancel()
					continue
				}

				log.Tracef("subscription details: %s, %s, %s, %d",
					diffID, notifyID, extraNonce1E, extraNonce2Size)

				m.extraNonce1 = extraNonce1E
				m.extraNonce2Size = extraNonce2Size
				m.subscribed = true

			case network.Submit:
				_, strErr, err := network.ParseSubmitWorkResponse(resp)
				if err != nil {
					log.Errorf("Parse submit response error: %v", err)
					m.cancel()
					continue
				}

				// log.Tracef("mining.submit status is %v", status)

				if strErr != nil {
					code, msg, err := network.ParseStratumError(strErr)
					if err != nil {
						log.Errorf("Failed to parse stratum error: %v", err)
						m.cancel()
						continue
					}

					log.Errorf("Stratum mining.submit error: [%d, %s, null]",
						code, msg)
					continue
				}

			default:
				log.Errorf("Unknown request method for response: %s", method)
			}

		case network.NotificationType:
			notif := msg.(*network.Request)
			switch notif.Method {
			case network.SetDifficulty:
				difficulty, err := network.ParseSetDifficultyNotification(notif)
				if err != nil {
					log.Errorf("Parse set difficulty response error: %v", err)
					m.cancel()
					continue
				}

				log.Tracef("difficulty is %v", difficulty)

				diff := new(big.Int).SetUint64(difficulty)
				target, err := dividend.DifficultyToTarget(m.config.net, diff)
				log.Tracef("target is %v", target)
				if err != nil {
					log.Errorf("Difficulty to target conversion error: %v", err)
					m.cancel()
					continue
				}

				m.workMtx.Lock()
				m.work.target = target
				m.workMtx.Unlock()

			case network.Notify:
				jobID, prevBlock, genTx1, genTx2, blockVersion, _, nTime, _, err :=
					network.ParseWorkNotification(notif)
				if err != nil {
					log.Errorf("Parse job notification error: %v", err)
					m.cancel()
					continue
				}

				blockHeader, err := network.GenerateBlockHeader(blockVersion,
					prevBlock, genTx1, nTime, m.extraNonce1, genTx2)
				if err != nil {
					log.Errorf("Generate block header error: %v", err)
					m.cancel()
					continue
				}

				headerB, err := blockHeader.Bytes()
				if err != nil {
					log.Errorf("Failed to get header bytes error: %v", err)
					m.cancel()
					continue
				}

				m.workMtx.Lock()
				m.work.jobID = jobID
				m.work.header = headerB
				m.workMtx.Unlock()

				// Notify the miner of received work.
				m.chainCh <- struct{}{}

			default:
				log.Errorf("Unknown method for notification: %s", notif.Method)
			}

		default:
			log.Errorf("Unknown message type received: %s", reqType)
		}
	}
}
