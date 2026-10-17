package client

// ClientWritableForTest lets the external integration fixture wait for the
// engine writer as well as the polling POST without exposing a runtime API.
func ClientWritableForTest(c *Client) bool {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	return c.state.conn != nil && c.state.conn.Conn.(interface{ Writable() bool }).Writable()
}
