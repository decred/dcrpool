// Copyright (c) 2019-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"time"

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
	id uint64 // update atomically.

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

// deleteRequest removes the provided request id from the id cache.
func (m *Miner) deleteRequest(id uint64) {
	m.reqMtx.Lock()
	delete(m.req, id)
	m.reqMtx.Unlock()
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
	req := pool.SubscribeRequest(&id, m.config.UserAgent, m.notifyID)
	err := m.encoder.Encode(req)
	if err != nil {
		return err
	}

	m.recordRequest(id, req.Method)

	return nil
}

// keepAlive checks the state of the connection to the pool and reconnects
// if needed. This should be run as a goroutine.
func (m *Miner) keepAlive(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			m.wg.Done()
			return

		default:
			m.connectedMtx.RLock()
			if m.connected {
				m.connectedMtx.RUnlock()
				continue
			}
			m.connectedMtx.RUnlock()

			time.Sleep(time.Second * 2)

			log.Infof("dialing %s...", m.config.Pool)

			conn, err := net.Dial("tcp", m.config.Pool)
			if err != nil {
				log.Errorf("unable to connect to %s: %v", m.config.Pool, err)
				continue
			}

			m.conn = conn
			m.encoder = json.NewEncoder(m.conn)
			m.reader = bufio.NewReader(m.conn)

			log.Infof("miner connected to %s", m.config.Pool)

			m.connectedMtx.Lock()
			m.connected = true
			m.connectedMtx.Unlock()

			err = m.subscribe()
			if err != nil {
				log.Errorf("unable to subscribe miner: %v", err)
				continue
			}
		}
	}
}

// read receives incoming data and passes the message received for
// processing. It must be run as a goroutine.
func (m *Miner) read(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			m.wg.Done()
			return
		default:
			// Non-blocking receive fallthrough.
		}

		// Proceed to read messages if the miner is connected.
		m.connectedMtx.RLock()
		if !m.connected {
			m.connectedMtx.RUnlock()
			continue
		}
		m.connectedMtx.RUnlock()

		data, err := m.reader.ReadBytes('\n')
		if err != nil {
			m.connectedMtx.Lock()
			m.connected = false
			m.connectedMtx.Unlock()

			m.workMtx.Lock()
			m.work = new(Work)
			m.workMtx.Unlock()

			// Signal the solver to abort hashing.
			select {
			case m.chainCh <- struct{}{}:
			default:
				// Non-blocking send fallthrough.
			}

			select {
			case <-ctx.Done():
				m.wg.Done()
				return
			default:
				// Non-blocking receive fallthrough.
			}

			if errors.Is(err, io.EOF) {
				continue
			}

			var nErr *net.OpError
			if errors.As(err, &nErr) {
				if nErr.Op == "read" && nErr.Net == "tcp" {
					continue
				}
			}

			log.Errorf("unable to read bytes: %v", err)
			continue
		}
		m.readCh <- data
	}
}

// listen reads and processes incoming messages from the pool client. It must
// be run as a goroutine.
func (m *Miner) process(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			if m.conn != nil {
				m.conn.Close()
			}

			m.wg.Done()
			return

		case data := <-m.readCh:
			msg, reqType, err := pool.IdentifyMessage(data)
			if err != nil {
				log.Errorf("message identification error: %v", err)
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
					log.Errorf("no request found for response "+
						"with id: %d", resp.ID)
					m.cancel()
					continue
				}

				// Remove the request ID since it is no longer needed.
				m.deleteRequest(resp.ID)

				switch method {
				case pool.Authorize:
					status, sErr, err := pool.ParseAuthorizeResponse(resp)
					if err != nil {
						log.Errorf("parse authorize response error: %v", err)
						m.cancel()
						continue
					}

					if sErr != nil {
						log.Errorf("authorize error: %v", sErr)
						m.cancel()
						continue
					}

					if !status {
						log.Error("authorization request failed for miner")
						m.cancel()
						continue
					}

					m.authorized = true

					log.Trace("Miner successfully authorized.")

				case pool.Subscribe:
					notifyID, extraNonce1E, extraNonce2Size, err :=
						pool.ParseSubscribeResponse(resp)
					if err != nil {
						log.Errorf("parse subscribe response error: %v", err)
						m.cancel()
						continue
					}

					log.Tracef("subscription details: %s, %s, %d",
						notifyID, extraNonce1E, extraNonce2Size)

					m.extraNonce1E = extraNonce1E
					m.extraNonce2Size = extraNonce2Size
					m.notifyID = notifyID
					m.subscribed = true

					err = m.authenticate()
					if err != nil {
						log.Errorf("unable to authenticate miner: %v", err)
						m.cancel()
						continue
					}

					log.Trace("Miner successfully subscribed.")

				case pool.Submit:
					accepted, sErr, err := pool.ParseSubmitWorkResponse(resp)
					if err != nil {
						log.Errorf("parse submit response error: %v", err)
						m.cancel()
						continue
					}

					if accepted {
						log.Trace("Submitted work was accepted by the network.")
					} else {
						log.Trace("Submitted work was rejected by the network.")
					}

					if sErr != nil {
						log.Errorf("mining.submit error: %v", sErr)
						continue
					}

				default:
					log.Errorf("unknown request method for response: %s", method)
				}

			case pool.NotificationMessage:
				notif := msg.(*pool.Request)
				switch notif.Method {
				case pool.SetDifficulty:
					difficulty, err := pool.ParseSetDifficultyNotification(notif)
					if err != nil {
						log.Errorf("parse set difficulty response error: %v", err)
						m.cancel()
						continue
					}

					log.Tracef("Difficulty is %v", difficulty)

					diff := new(big.Rat).SetUint64(difficulty)
					target := pool.DifficultyToTarget(m.config.net, diff)

					m.workMtx.Lock()
					m.work.target = target
					m.workMtx.Unlock()

				case pool.Notify:
					// Do not process work notifications if the miner is not
					// authorized or subscribed.
					if !m.authorized || !m.subscribed {
						continue
					}

					jobID, prevBlockE, genTx1E, genTx2E, blockVersionE, _, _,
						cleanJob, err := pool.ParseWorkNotification(notif)
					if err != nil {
						log.Errorf("parse job notification error: %v", err)
						m.cancel()
						continue
					}

					blockHeader, err := pool.GenerateBlockHeader(blockVersionE,
						prevBlockE, genTx1E, m.extraNonce1E, genTx2E)
					if err != nil {
						log.Errorf("generate block header error: %v", err)
						m.cancel()
						continue
					}

					headerB, err := blockHeader.Bytes()
					if err != nil {
						log.Errorf("unable to get header bytes error: %v", err)
						m.cancel()
						continue
					}

					m.workMtx.Lock()
					m.work.jobID = jobID
					m.work.header = headerB
					m.workMtx.Unlock()

					if cleanJob {
						log.Tracef("received work for block #%d", blockHeader.Height)

						if m.config.Stall {
							log.Tracef("purposefully stalling on work")
							continue
						}

						// Notify the miner of received work.
						select {
						case m.chainCh <- struct{}{}:
						default:
							// Non-blocking send fallthrough.
						}
					}

				default:
					log.Errorf("unknown method for notification: %s", notif.Method)
				}

			default:
				log.Errorf("unknown message type received: %s", reqType)
			}
		}
	}
}

// run handles the process life cycles of the miner.
func (m *Miner) run(ctx context.Context) {
	m.wg.Add(3)
	go m.read(ctx)
	go m.keepAlive(ctx)
	go m.process(ctx)

	if !m.config.Stall {
		m.wg.Add(3)
		go m.core.solve(ctx)
		go m.core.hashRateMonitor(ctx)
		go m.core.generateBlocks(ctx)
	}

	m.wg.Wait()
	log.Infof("Miner terminated.")
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
