package main

import (
	"bytes"
	"context"
	"dnldd/dcrpool/ws"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/wire"

	"github.com/gorilla/websocket"
)

const (
	scheme   = "ws"
	endpoint = "ws"
)

// Work represents the data recieved from a work notification. It comprises of
// hex encoded block header and target data.
type Work struct {
	header []byte
	target []byte
}

// Client connects a miner to the mining pool for block template updates
// and work submissions.
type Client struct {
	workHeight uint32
	currWork   *Work
	config     *config
	closed     uint64
	ctx        context.Context
	cancel     context.CancelFunc
	Conn       *websocket.Conn
	workMtx    sync.RWMutex
	CPUMiner   *CPUMiner
}

// newClient initializes a mining pool client.
func newClient(config *config) (*Client, error) {
	conn, err := dial(config)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.TODO())
	c := &Client{
		config: config,
		Conn:   conn,
		ctx:    ctx,
		cancel: cancel,
	}

	if config.Generate {
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
			pc.Conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			time.Sleep(time.Second * 5)
			break out
		default:
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
					pc.Conn.WriteJSON(ws.PongResponse(req.ID))
					if err != nil {
						log.Errorf("Websockect write error: %v", err)
						pc.cancel()
						continue
					}
				}
			case ws.ResponseType:
			case ws.NotificationType:
				req := msg.(*ws.Request)
				switch req.Method {
				case ws.Work:
					work, err := parseWorkNotification(req)
					if err != nil {
						pc.cancel()
						continue
					}

					blkHeight, err := ws.FetchBlockHeight(work.header)
					if err != nil {
						pc.cancel()
						continue
					}

					workHeight := atomic.LoadUint32(&pc.workHeight)

					// Accept the work data if the miner is currently starting
					// out, if the received work data has an incremented block
					// height, or if the received work data is an updated work
					// data at the current height.
					if workHeight == 0 || blkHeight == workHeight+1 || blkHeight == workHeight {
						pc.workMtx.Lock()
						pc.currWork = work
						pc.workMtx.Unlock()
						atomic.StoreUint32(&pc.workHeight, blkHeight)
						log.Debugf("Work data updated, current height is %v",
							blkHeight)
					}

				case ws.ConnectedBlock:
					blkHeight, err := parseConnectedBlockNotification(req)
					if err != nil {
						pc.cancel()
						continue
					}

					// Stop current work and wait for a new block template.
					pc.workMtx.Lock()
					pc.currWork = nil
					pc.workMtx.Unlock()

					atomic.StoreUint32(&pc.workHeight, blkHeight+1)

				case ws.DisconnectedBlock:
					blkHeight, err := parseDisconnectedBlockNotification(req)
					if err != nil {
						pc.cancel()
						continue
					}

					// Stop current work and wait for a new block template.
					pc.workMtx.Lock()
					pc.currWork = nil
					pc.workMtx.Unlock()

					atomic.StoreUint32(&pc.workHeight, blkHeight)
				}
			default:
				log.Debugf("Unknowning message type received")
			}
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

// parseWorkNotification parses a work notification message.
func parseWorkNotification(req *ws.Request) (*Work, error) {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return nil, errors.New("Invalid work notification, should have " +
			"'params' field")
	}

	header, ok := params["header"].(string)
	if !ok {
		return nil, errors.New("Invalid work notification, 'params' data " +
			"should have 'header' field")
	}

	target, ok := params["target"].(string)
	if !ok {
		return nil, errors.New("Invalid work notification, 'params' data " +
			"should have 'target' field")
	}

	return &Work{
		header: []byte(header),
		target: []byte(target),
	}, nil
}

// parseConnectedBlockNotification parses a connected block notification
// message.
func parseConnectedBlockNotification(req *ws.Request) (uint32, error) {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return 0, errors.New("Invalid connected block notification, should " +
			"have 'params' field")
	}

	height, ok := params["height"].(float64)
	if !ok {
		return 0, errors.New("Invalid connected block notification, " +
			"'params' data should have 'height' field")
	}

	return uint32(height), nil
}

// parseDisconnectedBlockNotification parses a disconnected block notification
// message.
func parseDisconnectedBlockNotification(req *ws.Request) (uint32, error) {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return 0, errors.New("Invalid disconnected block notification, should " +
			"have 'params' field")
	}

	height, ok := params["height"].(float64)
	if !ok {
		return 0, errors.New("Invalid disconnected block notification, " +
			"'params' data should have 'height' field")
	}

	return uint32(height), nil
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
