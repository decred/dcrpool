package main

import (
	"context"
	"dnldd/dcrpool/ws"
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
					websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
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

	// Dial the connection.
	url := fmt.Sprintf("%s://%s/%s", scheme, cfg.Host, endpoint)
	wsConn, resp, err := dialer.Dial(url, nil)
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
