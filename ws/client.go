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
	ch          chan Message
	mtx         sync.RWMutex
	Ctx         context.Context
	cancel      context.CancelFunc
	ip          string
	ticker      *time.Ticker
	pingRetries uint64
}

// NewClient initializes a new websocket client.
func NewClient(h *Hub, socket *websocket.Conn, ip string) *Client {
	ctx, cancel := context.WithCancel(context.TODO())
	atomic.AddUint64(&h.ConnCount, 1)
	return &Client{
		hub:         h,
		ws:          socket,
		ip:          ip,
		ch:          make(chan Message),
		Ctx:         ctx,
		cancel:      cancel,
		ticker:      time.NewTicker(time.Second * 5),
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

			// Ensure the requesting client is within their request limits.
			allow := c.hub.limiter.WithinLimit(c.ip)
			if !allow {
				c.ch <- tooManyRequestsResponse(c.nextID())
				continue
			}

			msg, reqType, err := IdentifyMessage(data)
			if err != nil {
				log.Debug(err)
				c.cancel()
				continue
			}

			switch reqType {
			case RequestType:
				req := msg.(*Request)
				switch req.Method {
				// Process message.
				}
			case ResponseType:
				// The only response type expected from connected clients are
				// ping responses.
				processPing(c, msg)
			case NotificationType:
			default:
				log.Errorf("Unknown message type received")
			}
		}
	}
}

// Send messages to a client.
func (c *Client) Send(ctx context.Context) {
out:
	for {
		select {
		case <-ctx.Done():
			c.ticker.Stop()
			c.ws.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
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

			err := c.ws.WriteJSON(PingRequest(c.nextID()))
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

			err := c.ws.WriteJSON(msg)
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

			err := c.ws.WriteJSON(msg)
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
