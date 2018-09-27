package ws

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Client defines an established websocket connection.
type Client struct {
	hub         *Hub
	id          uint64
	ws          *websocket.Conn
	wsMtx       sync.RWMutex
	req         map[uint64]string
	reqMtx      sync.RWMutex
	ch          chan Message
	Ctx         context.Context
	cancel      context.CancelFunc
	ip          string
	ticker      *time.Ticker
	pingRetries uint64
}

// recordRequest logs the client request as an id/method pair.
func (c *Client) recordRequest(id uint64, method string) {
	c.reqMtx.Lock()
	c.req[id] = method
	c.reqMtx.Unlock()
}

// fetchRequest fetches the request method of the provided request id.
func (c *Client) fetchRequest(id uint64) string {
	var method string
	c.reqMtx.RLock()
	method = c.req[id]
	c.reqMtx.RUnlock()
	return method
}

// deleteRequest removes the request referenced by the provided id.
func (c *Client) deleteRequest(id uint64) {
	c.reqMtx.Lock()
	delete(c.req, id)
	c.reqMtx.Unlock()
}

// NewClient initializes a new websocket client.
func NewClient(h *Hub, socket *websocket.Conn, ip string, ticker *time.Ticker) *Client {
	ctx, cancel := context.WithCancel(context.TODO())
	atomic.AddUint64(&h.ConnCount, 1)
	return &Client{
		hub:         h,
		ws:          socket,
		ip:          ip,
		ch:          make(chan Message),
		Ctx:         ctx,
		cancel:      cancel,
		ticker:      ticker,
		req:         make(map[uint64]string, 0),
		pingRetries: 0,
	}
}

// Process parses and handles inbound client messages.
func (c *Client) Process(ctx context.Context) {
	c.ws.SetReadLimit(MaxMessageSize)
out:
	for {
		select {
		case <-ctx.Done():
			// decrement the connection counter.
			atomic.AddUint64(&c.hub.ConnCount, ^uint64(0))
			break out
		default:
			// Non-blocking receive fallthrough.
		}

		// Wait for a message.
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway, websocket.CloseAbnormalClosure,
				websocket.CloseNormalClosure) {
				log.Errorf("Websocket read error: %v", err)
			}
			c.cancel()
			continue
		}

		msg, reqType, err := IdentifyMessage(data)
		if err != nil {
			log.Debug(err)
			c.cancel()
			continue
		}

		// TODO: More research needs to be done before applying this.
		// Ensure the requesting client is within their request limits.
		// allow := c.hub.limiter.WithinLimit(c.ip)
		// if !allow {
		// 	switch reqType {
		// 	case RequestType:
		// 		req := msg.(*Request)
		// 		c.ch <- tooManyRequestsResponse(req.ID)
		// 	case ResponseType:
		// 		resp := msg.(*Response)
		// 		c.ch <- tooManyRequestsResponse(resp.ID)
		// 	case NotificationType:
		// 		// Clients are not allowed to send notifications.
		// 		c.cancel()
		// 		continue
		// 	}
		// }

		switch reqType {
		case RequestType:
			req := msg.(*Request)
			switch req.Method {
			case SubmittedWork:
				header, err := ParseWorkSubmissionRequest(req)
				if err != nil {
					log.Debug(err)
					msg := err.Error()

					c.ch <- EvaluatedWorkResponse(req.ID, &msg, false)
				}

				status, err := c.hub.SubmitWork(header)
				log.Debugf("Submitted work status: %v", status)

				var resp *Response
				if err != nil {
					msg := err.Error()
					resp = EvaluatedWorkResponse(req.ID, &msg, status)
				}

				resp = EvaluatedWorkResponse(req.ID, nil, status)
				c.ch <- resp
			default:
				log.Debugf("Unknowning request type received")
			}

		case ResponseType:
			resp := msg.(*Response)
			method := c.fetchRequest(*resp.ID)
			if method == "" {
				log.Error("No request found for received response "+
					" with id: ", *resp.ID)
				continue
			}

			switch method {
			case Ping:
				c.deleteRequest(*resp.ID)
				atomic.StoreUint64(&c.pingRetries, 0)
			default:
				log.Debugf("Unknowning response type received")
			}

		case NotificationType:
		default:
			log.Errorf("Unknown message type received")
		}
	}
}

// Send messages to a client.
func (c *Client) Send(ctx context.Context) {
out:
	for {
		select {
		case <-ctx.Done():
			c.wsMtx.Lock()
			c.ws.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			c.wsMtx.Unlock()
			close(c.ch)
			break out
		case <-c.ticker.C:
			// Close the connection if unreachable after 3 pings.
			retries := atomic.LoadUint64(&c.pingRetries)
			if retries == MaxPingRetries {
				log.Debugf("Client %v unreachable, closing connection.", c.ip)
				c.cancel()
				continue
			}

			id := c.nextID()
			c.recordRequest(*id, Ping)
			c.wsMtx.Lock()
			err := c.ws.WriteJSON(PingRequest(id))
			c.wsMtx.Unlock()
			if err != nil {
				log.Error(err)
				c.cancel()
				continue
			}
			atomic.AddUint64(&c.pingRetries, 1)
		case msg := <-c.ch:
			// Close the connection on receiving a nil message reference.
			if msg == nil {
				c.cancel()
				continue
			}

			c.wsMtx.Lock()
			err := c.ws.WriteJSON(msg)
			c.wsMtx.Unlock()
			if err != nil {
				log.Error(err)
				c.cancel()
				continue
			}
		case msg := <-c.hub.Broadcast:
			// Close the connection on receiving a nil message reference.
			if msg == nil {
				c.cancel()
				continue
			}

			c.wsMtx.Lock()
			err := c.ws.WriteJSON(msg)
			c.wsMtx.Unlock()
			if err != nil {
				log.Error(err)
				c.cancel()
				continue
			}
		}
	}
}

// nextID returns the next message id for the client.
func (c *Client) nextID() *uint64 {
	id := atomic.AddUint64(&c.id, 1)
	return &id
}
