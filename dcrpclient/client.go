package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/wire"
	"github.com/gorilla/websocket"

	"dnldd/dcrpool/worker"
	"dnldd/dcrpool/ws"
)

const (
	scheme   = "ws"
	endpoint = "ws"
)

// Work represents the data recieved from a work notification. It comprises of
// hex encoded block header and pool target data.
type Work struct {
	header []byte
	target []byte
}

// Client connects a miner to the mining pool for block template updates
// and work submissions.
type Client struct {
	id       uint64
	req      map[uint64]string
	reqMtx   sync.RWMutex
	work     *Work
	workMtx  sync.RWMutex
	config   *config
	closed   uint64
	ctx      context.Context
	cancel   context.CancelFunc
	Conn     *websocket.Conn
	connMtx  sync.Mutex
	chainCh  chan struct{}
	CPUMiner *CPUMiner
}

// nextID returns the next message id for the client.
func (pc *Client) nextID() *uint64 {
	id := atomic.AddUint64(&pc.id, 1)
	return &id
}

// recordRequest logs the client request as an id/method pair.
func (pc *Client) recordRequest(id uint64, method string) {
	pc.reqMtx.Lock()
	pc.req[id] = method
	pc.reqMtx.Unlock()
}

// fetchRequest fetches the request method of the provided request id.
func (pc *Client) fetchRequest(id uint64) string {
	var method string
	pc.reqMtx.RLock()
	method = pc.req[id]
	pc.reqMtx.RUnlock()
	return method
}

// deleteRequest removes the request referenced by the provided id.
func (pc *Client) deleteRequest(id uint64) {
	pc.reqMtx.Lock()
	delete(pc.req, id)
	pc.reqMtx.Unlock()
}

// newClient initializes a mining pool client.
func newClient(config *config) (*Client, error) {
	conn, err := dial(config)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.TODO())
	c := &Client{
		config:  config,
		Conn:    conn,
		ctx:     ctx,
		cancel:  cancel,
		req:     make(map[uint64]string, 0),
		chainCh: make(chan struct{}, 0),
	}

	if config.MinerType == worker.CPU {
		c.CPUMiner = newCPUMiner(c)
		c.CPUMiner.Start()
		log.Info("Started CPU miner.")
	}

	return c, nil
}

// process fetches incoming messages and handles them accordingly.
func (pc *Client) processMessages() {
out:
	for {
		select {
		case <-pc.ctx.Done():
			// Send a close message.
			pc.connMtx.Lock()
			pc.Conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			pc.connMtx.Unlock()
			time.Sleep(time.Second * 5)
			break out
		default:
			// Non blocking receive fallthrough.
		}

		_, data, err := pc.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway, websocket.CloseAbnormalClosure,
				websocket.CloseNormalClosure) {
				log.Errorf("Websocket read error: %v", err)
			}
			pc.cancel()
			continue
		}

		// Identify the message type and proceed to handle accordingly.
		msg, reqType, err := ws.IdentifyMessage(data)
		if err != nil {
			log.Errorf("Websocket message error: %v", err)
			pc.cancel()
			continue
		}

		switch reqType {
		case ws.RequestType:
			req := msg.(*ws.Request)
			switch req.Method {
			case ws.Ping:
				pc.connMtx.Lock()
				pc.Conn.WriteJSON(ws.PongResponse(req.ID))
				pc.connMtx.Unlock()
				if err != nil {
					log.Errorf("Websockect write error: %v", err)
					pc.cancel()
					continue
				}
			}

		case ws.ResponseType:
			resp := msg.(*ws.Response)
			method := pc.fetchRequest(*resp.ID)
			if method == "" {
				log.Error("No request found for received response "+
					" with id: ", *resp.ID)
				continue
			}

			switch method {
			case ws.SubmitWork:
				pc.deleteRequest(*resp.ID)
				accepted, err := ws.ParseEvaluatedWorkResponse(resp)
				if err != nil {
					log.Error(err)
					continue
				}
				log.Debugf("Evaluated work accepted: %v", accepted)
			}

		case ws.NotificationType:
			req := msg.(*ws.Request)
			switch req.Method {
			case ws.Work:
				header, target, err := ws.ParseWorkNotification(req)
				if err != nil {
					log.Error(err)
					pc.cancel()
					continue
				}

				pc.workMtx.Lock()
				pc.work = &Work{
					header: header,
					target: target,
				}
				pc.workMtx.Unlock()

				// Update miner of new block template.
				pc.chainCh <- struct{}{}

			case ws.ConnectedBlock, ws.DisconnectedBlock:
				pc.workMtx.Lock()
				pc.work = nil
				pc.workMtx.Unlock()

				// Update miner of connected/disconnected block.
				pc.chainCh <- struct{}{}
			default:
				log.Debugf("Unknowning notification type received")
			}

		default:
			log.Debugf("Unknowning message type received")
		}
	}
	os.Exit(1)
}

// dial opens a websocket connection using the provided connection configuration
// details.
func dial(cfg *config) (*websocket.Conn, error) {
	// Create the websocket dialer.
	dialer := new(websocket.Dialer)
	dialer.ReadBufferSize = ws.MaxMessageSize
	dialer.WriteBufferSize = ws.MaxMessageSize

	// Set the authorization header.
	credentials := fmt.Sprintf("%s:%s", cfg.User, cfg.Pass)
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(credentials))
	header := make(http.Header)
	header.Add("Authorization", auth)

	// Set the miner type.
	header.Add("Miner", cfg.MinerType)

	// Dial the connection.
	url := fmt.Sprintf("%s://%s/%s", scheme, cfg.Host, endpoint)
	wsConn, resp, err := dialer.Dial(url, header)
	if err != nil {
		if err != websocket.ErrBadHandshake || resp == nil {
			return nil, err
		}

		// Detect HTTP authentication error status codes.
		if resp.StatusCode == http.StatusUnauthorized ||
			resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("Authentication failure: %v", err)
		}

		// The connection was authenticated and the status response was
		// ok, but the websocket handshake still failed, so the endpoint
		// is invalid in some way.
		if resp.StatusCode == http.StatusOK {
			return nil, fmt.Errorf("Invalid endpoint: %v", err)
		}

		// Return the status text from the server if none of the special
		// cases above apply.
		return nil, fmt.Errorf("Connection error: %v, %v", err, resp.Status)
	}

	return wsConn, nil
}

// fetchBlockHeader deserializes the block header from the provided hex
// encoded header data.
func fetchBlockHeader(encoded []byte) (*wire.BlockHeader, error) {
	data := []byte(encoded)
	decoded := make([]byte, len(data))
	_, err := hex.Decode(decoded, data)
	if err != nil {
		return nil, err
	}

	// Deserialize the block header.
	var header wire.BlockHeader
	reader := bytes.NewReader(decoded[:180])
	err = header.Deserialize(reader)
	if err != nil {
		return nil, err
	}

	return &header, nil
}
