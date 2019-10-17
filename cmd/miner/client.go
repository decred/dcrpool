// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/davecgh/go-spew/spew"

	"github.com/decred/dcrpool/pool"
)

// Work represents the data received from a work notification. It comprises of
// hex encoded block header and pool target data.
type Work struct {
	jobID  string
	header []byte
	target *big.Rat
}

// Miner represents a stratum mining client.
type Miner struct {
	id uint64 // update atomically

	conn            net.Conn
	core            *CPUMiner
	encoder         *json.Encoder
	reader          *bufio.Reader
	work            *Work
	workMtx         sync.RWMutex
	config          *config
	req             map[uint64]string
	reqMtx          sync.RWMutex
	chainCh         chan struct{}
	readCh          chan []byte
	authorized      bool
	subscribed      bool
	connected       bool
	connectedMtx    sync.RWMutex
	started         int64
	cancel          context.CancelFunc
	extraNonce1E    string
	extraNonce2Size uint64
	notifyID        string
	wg              sync.WaitGroup
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

// nextID returns the next message id for the client.
func (m *Miner) nextID() uint64 {
	return atomic.AddUint64(&m.id, 1)
}

// authenticate sends a stratum miner authentication message.
func (m *Miner) authenticate() error {
	id := m.nextID()
	req := pool.AuthorizeRequest(&id, m.config.User, m.config.Address)
	err := m.encoder.Encode(req)
	if err != nil {
		return err
	}

	m.recordRequest(id, req.Method)

	return nil
}

// subscribe sends a stratum miner subscribe message.
func (m *Miner) subscribe() error {
	id := m.nextID()
	req := pool.SubscribeRequest(&id, "cpuminer", version(), m.notifyID)
	err := m.encoder.Encode(req)
	if err != nil {
		return err
	}

	m.recordRequest(id, req.Method)

	return nil
}

// keepAlive checks the state of the connection to the pool and reconnects
// if needed. This should be run as a goroutine.
func (m *Miner) keepAlive() {
	for {
		m.connectedMtx.RLock()
		if m.connected {
			m.connectedMtx.RUnlock()
			time.Sleep(time.Second)
			continue
		}
		m.connectedMtx.RUnlock()

		poolAddr := strings.Replace(m.config.Pool, "stratum+tcp", "", 1)
		conn, err := net.Dial("tcp", poolAddr)
		if err != nil {
			log.Errorf("unable connect to %s, %v", poolAddr, err)
			time.Sleep(time.Second * 5)
			continue
		}

		m.conn = conn
		m.encoder = json.NewEncoder(m.conn)
		m.reader = bufio.NewReader(m.conn)

		err = m.subscribe()
		if err != nil {
			log.Errorf("unable to subscribe miner: %v", err)
			time.Sleep(time.Second * 5)
			continue
		}

		err = m.authenticate()
		if err != nil {
			log.Errorf("unable to authenticate miner: %v", err)
			time.Sleep(time.Second * 5)
			continue
		}

		m.connectedMtx.Lock()
		m.connected = true
		m.connectedMtx.Unlock()

		time.Sleep(time.Second * 5)
	}
}

// read receives incoming data and passes the message received for
// processing. It must be run as a goroutine.
func (m *Miner) read() {
	for {
		// Read only if the miner is connected.
		m.connectedMtx.RLock()
		if !m.connected {
			m.connectedMtx.RUnlock()
			time.Sleep(time.Second)
			continue
		}
		m.connectedMtx.RUnlock()

		data, err := m.reader.ReadBytes('\n')
		if err != nil {
			m.workMtx.Lock()
			m.work = new(Work)
			m.workMtx.Unlock()

			m.connectedMtx.Lock()
			m.connected = false
			m.connectedMtx.Unlock()

			if err == io.EOF {
				continue
			}

			if nErr := err.(*net.OpError); nErr != nil {
				if nErr.Op == "read" && nErr.Net == "tcp" {
					continue
				}
			}

			log.Errorf("Failed to read bytes: %v", err)
			continue
		}

		log.Tracef("Message received is: %v", spew.Sdump(string(data)))

		m.readCh <- data
	}
}

// listen reads and processes incoming messages from the pool client. It must
// be run as a goroutine.
func (m *Miner) process(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			m.conn.Close()
			m.wg.Done()
			return

		case data := <-m.readCh:
			msg, reqType, err := pool.IdentifyMessage(data)
			if err != nil {
				log.Errorf("Message identification error: %v", err)
				m.cancel()
				continue
			}

			switch reqType {
			case pool.RequestMessage:
				req := msg.(*pool.Request)
				switch req.Method {
				// Process requests from the mining pool. There are none
				// expected currently.
				}

			case pool.ResponseMessage:
				resp := msg.(*pool.Response)
				method := m.fetchRequest(resp.ID)
				if method == "" {
					log.Error("No request found for response with id: ", resp.ID,
						spew.Sdump(resp))
					m.cancel()
					continue
				}

				switch method {
				case pool.Authorize:
					status, errStr, err := pool.ParseAuthorizeResponse(resp)
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

				case pool.Subscribe:
					diffID, notifyID, extraNonce1E, extraNonce2Size, err :=
						pool.ParseSubscribeResponse(resp)
					if err != nil {
						log.Errorf("Parse subscribe response error: %v", err)
						m.cancel()
						continue
					}

					log.Tracef("subscription details: %s, %s, %s, %d",
						diffID, notifyID, extraNonce1E, extraNonce2Size)

					m.extraNonce1E = extraNonce1E
					m.extraNonce2Size = extraNonce2Size
					m.notifyID = notifyID
					m.subscribed = true

				case pool.Submit:
					accepted, sErr, err := pool.ParseSubmitWorkResponse(resp)
					if err != nil {
						log.Errorf("Parse submit response error: %v", err)
						m.cancel()
						continue
					}

					log.Tracef("Accepted status is %v", accepted)

					if sErr != nil {
						log.Errorf("Stratum mining.submit error: [%d, %s, %s]",
							sErr.Code, sErr.Message, sErr.Traceback)
						continue
					}

				default:
					log.Errorf("Unknown request method for response: %s", method)
				}

			case pool.NotificationMessage:
				notif := msg.(*pool.Request)
				switch notif.Method {
				case pool.SetDifficulty:
					difficulty, err := pool.ParseSetDifficultyNotification(notif)
					if err != nil {
						log.Errorf("Parse set difficulty response error: %v", err)
						m.cancel()
						continue
					}

					log.Tracef("Difficulty is %v", difficulty)

					// Switch to big.Rat.SetUint64() when go1.13 is the
					// minimum supported for test builds.
					diff := new(big.Rat).SetInt(new(big.Int).SetUint64(difficulty))
					target, err := pool.DifficultyToTarget(m.config.net, diff)
					if err != nil {
						log.Errorf("Difficulty to target conversion error: %v", err)
						m.cancel()
						continue
					}

					log.Tracef("Target is %v", target.FloatString(4))

					m.workMtx.Lock()
					m.work.target = target
					m.workMtx.Unlock()

				case pool.Notify:
					// Do not process work notifications if the miner is not
					// authorized or subscribed.
					if !m.authorized || !m.subscribed {
						continue
					}

					jobID, prevBlockE, genTx1E, genTx2E, blockVersionE, _, _, _, err :=
						pool.ParseWorkNotification(notif)
					if err != nil {
						log.Errorf("Parse job notification error: %v", err)
						m.cancel()
						continue
					}

					blockHeader, err := pool.GenerateBlockHeader(blockVersionE,
						prevBlockE, genTx1E, m.extraNonce1E, genTx2E)
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

					log.Tracef("Block header is: %v", spew.Sdump(blockHeader))

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
}

// run handles the process life cycles of the miner.
func (m *Miner) run(ctx context.Context) {
	go m.read()
	go m.keepAlive()
	go m.core.solve(ctx)

	m.wg.Add(3)
	go m.process(ctx)
	go m.core.hashRateMonitor(ctx)
	go m.core.generateBlocks(ctx)
	m.wg.Wait()
}

// NewMiner creates a stratum mining client.
func NewMiner(cfg *config, cancel context.CancelFunc) *Miner {
	m := &Miner{
		config:  cfg,
		work:    new(Work),
		cancel:  cancel,
		chainCh: make(chan struct{}),
		readCh:  make(chan []byte),
		req:     make(map[uint64]string),
		started: time.Now().Unix(),
	}

	m.core = NewCPUMiner(m)
	return m
}
