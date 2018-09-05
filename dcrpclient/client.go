package main

import (
	"context"
	"crypto/tls"
	"dnldd/dcrpool/ws"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

const (
	scheme = "wss"
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
			pc.Conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			time.Sleep(time.Second * 5)
			break out
		default:
			_, data, err := pc.Conn.ReadMessage()
			if err != nil {
				log.Error(err)
				pc.cancel()
				continue
			}

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
					log.Debug("responding")
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

// handleInterrupt gracefully terminates the pool client when a interrupt signal
// is recieved.
// func (pc *Client) processInterrupt(interrupt chan os.Signal,
// 	cancel context.CancelFunc) {

// }

// dial opens a websocket connection using the passed connection configuration
// details.
func dial(cfg *config) (*websocket.Conn, error) {
	var tlsConfig *tls.Config
	var scheme = "wss"
	tlsConfig = &tls.Config{
		Certificates:       []tls.Certificate{cfg.certificate},
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	}
	// if len(cfg.cert) > 0 {
	// 	pool := x509.NewCertPool()
	// 	pool.AppendCertsFromPEM(cfg.cert)
	// 	tlsConfig.RootCAs = pool
	// }

	// Create the websocket dialer.
	dialer := websocket.Dialer{TLSClientConfig: tlsConfig}
	dialer.ReadBufferSize = ws.MaxMessageSize
	dialer.WriteBufferSize = ws.MaxMessageSize

	// TODO: Authenticate the connection
	// The mining pool server requires basic authorization, so create a custom
	// request header with the Authorization header set.
	// login := config.User + ":" + config.Pass
	// auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(login))
	// requestHeader := make(http.Header)
	// requestHeader.Add("Authorization", auth)

	// Dial the connection.
	url := fmt.Sprintf("%s://%s/%s", scheme, cfg.Host, "ws")
	// wsConn, resp, err := dialer.Dial(url, requestHeader)
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
