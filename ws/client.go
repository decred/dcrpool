package ws

import (
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxMessageSize = 256
	maxPingRetries = 3
)

// Client defines an established websocket connection.
type Client struct {
	hub           *Hub
	ws            *websocket.Conn
	ch            chan Message
	ticker        *time.Ticker
	pingRetries   int
	Authenticated bool
}

// NewClient initializes a new wesocket client.
func NewClient(h *Hub, socket *websocket.Conn) *Client {
	return &Client{
		hub:           h,
		ws:            socket,
		ch:            make(chan Message),
		ticker:        time.NewTicker(time.Second * 30),
		pingRetries:   0,
		Authenticated: false,
	}
}

// Process parses and handles inbound client messages.
func (c *Client) Process() {
	var castReq = func(data []byte) *Request {
		var req Request
		err := json.Unmarshal(data, &req)
		if err != nil {
			return nil
		}
		return &req
	}

	var castResp = func(data []byte) *Response {
		var resp Response
		err := json.Unmarshal(data, &resp)
		if err != nil {
			return nil
		}
		return &resp
	}

	c.ws.SetReadLimit(maxMessageSize)
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseNormalClosure) {
				log.Error(err)
			}
			c.hub.close <- c
			break
		}

		var req *Request
		var resp *Response

		req = castReq(data)
		if req == nil {
			resp = castResp(data)
			if resp == nil {
				c.hub.close <- c
				break
			}
		}

		if req != nil {
			switch req.Method {

			}
		}

		if resp != nil {
			processPing(c, req)
		}
	}
}

// Send routes messages to a client.
func (c *Client) Send() {
	for {
		select {
		case <-c.ticker.C:
			// Close the connection if unreachable after 3 pings.
			if c.pingRetries == maxPingRetries {
				c.hub.close <- c
				break
			}

			err := c.ws.WriteJSON(pingReq)
			if err != nil {
				log.Error(err)
				c.hub.close <- c
				break
			}
			c.pingRetries++
		case msg := <-c.ch:
			// Close the connection on receiving a nil message reference.
			if msg == nil {
				c.hub.close <- c
				break
			}

			err := c.ws.WriteJSON(msg)
			if err != nil {
				log.Error(err)
				c.hub.close <- c
				break
			}
		}
	}
}

// remove closes the active websocket connection and terminates the client.
func (c *Client) remove() {
	c.ticker.Stop()
	c.ws.WriteMessage(websocket.CloseMessage, []byte{})
	close(c.ch)
	delete(c.hub.clients, c)
}
