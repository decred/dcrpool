package ws

import (
	"sync/atomic"
)

func processPing(c *Client, m Message) {
	resp := m.(*Response)
	if resp == nil {
		c.cancel()
		return
	}

	msg := resp.Result.(string)
	if msg != Pong {
		c.cancel()
		return
	}

	atomic.StoreUint64(&c.pingRetries, 0)
}
