package client

// ClientWritableForTest lets the external integration fixture wait for the
// engine writer as well as the polling POST without exposing a runtime API.
func ClientWritableForTest(c *Client) bool {
	c.state.mu.Lock()
	current := c.state.conn
	c.state.mu.Unlock()
	return current != nil && current.Conn.(interface{ Writable() bool }).Writable()
}
