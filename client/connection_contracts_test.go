package client

func clientWritable(c *manager) bool {
	c.state.mu.Lock()
	current := c.state.conn
	c.state.mu.Unlock()
	return current != nil && current.Conn.(interface{ Writable() bool }).Writable()
}
