package connection

import (
	"github.com/sshaplygin/go-socket.io/client/internal/namespace"
)

type Conn = namespace.Conn
type Handshake = namespace.Handshake
type AckFunc = namespace.AckFunc
type Emitter = namespace.Emitter

func clientWritable(c *Manager) bool {
	c.state.mu.Lock()
	current := c.state.conn
	c.state.mu.Unlock()
	return current != nil && current.Conn.(interface{ Writable() bool }).Writable()
}
