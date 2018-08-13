package ws

func processPing(c *Client, m Message) {
	resp := m.(*Response)
	if resp == nil {
		c.hub.close <- c
		return
	}

	msg := resp.Result.(string)
	if msg != pong {
		c.hub.close <- c
		return
	}

	c.pingRetries = 0
}
