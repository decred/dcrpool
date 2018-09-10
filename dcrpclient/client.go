package main

import (
	"context"
	"dnldd/dcrpool/ws"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

const (
	scheme   = "ws"
	endpoint = "ws"
)

// Client connects a miner to the mining pool for block template updates
// and work submissions.
type Client struct {
	Config *config
	closed uint64
	ctx    context.Context
	cancel context.CancelFunc
	Conn   *websocket.Conn
}

// newClient initializes a mining pool client.
func newClient(config *config) (*Client, error) {
	conn, err := dial(config)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.TODO())
	return &Client{
		Config: config,
		Conn:   conn,
		ctx:    ctx,
		cancel: cancel,
	}, nil
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
					params, ok := req.Params.(map[string]interface{})
					if !ok {
						log.Errorf("Invalid work notification " +
							", should have 'params' field")
						pc.cancel()
						continue
					}

					header, ok := params["header"].(string)
					if !ok {
						log.Errorf("Invalid work notification " +
							", 'params' should have 'header' field")
						pc.cancel()
						continue
					}

					hBytes := []byte(header)
					dst := make([]byte, len(hBytes))
					hex.Decode(dst, hBytes)
					height := fetchBlockHeight(dst)
					log.Debugf("block height is %v", height)
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

// fetchBlockHeight retrieves the block height from the provided hex encoded
// work data.
func fetchBlockHeight(decodedWork []byte) uint32 {
	return binary.LittleEndian.Uint32(decodedWork[128:133])
}

// fetchNonce retrieves the 10-byte nonce miners modify to mine a block via a
// mining pool. This is sans the 2-byte mining pool assigned miner ID.
func fetchNonce(decodedWork []byte) []byte {
	return decodedWork[136:147]
}

// setNonce applies the 10-byte miner generated nonce to decoded work data.
func setNonce(decodedWork []byte, nonce [10]byte) {
	copy(decodedWork[140:171], nonce[:])
}

// fetchID retrieves the mining pool assigned 2-byte miner ID in decoded
// work data.
func fetchID(decodedWork []byte) []byte {
	return decodedWork[140:171]
}

// setID applies the mining pool assigned 2-byte miner ID to decoded work data.
func setID(decodedWork []byte, ID [2]byte) {
	copy(decodedWork[140:171], ID[:])
}
