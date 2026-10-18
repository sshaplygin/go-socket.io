package connection

import (
	"time"
)

type clientObservers struct {
	attempt   func(int)
	reconnect func(int)
	err       func(error)
	failed    func()
	ping      func()
	pong      func(time.Duration)
}

// OnReconnectAttempt sets the callback before each automatic dial; nil removes it.
func (c *Manager) OnReconnectAttempt(f func(attempt int)) {
	c.state.observersMu.Lock()
	c.state.observers.attempt = f
	c.state.observersMu.Unlock()
}

// OnReconnect runs after the engine opens, before namespace CONNECT packets are sent.
func (c *Manager) OnReconnect(f func(attempt int)) {
	c.state.observersMu.Lock()
	c.state.observers.reconnect = f
	c.state.observersMu.Unlock()
}

// OnReconnectError sets the callback for a failed automatic dial; nil removes it.
func (c *Manager) OnReconnectError(f func(err error)) {
	c.state.observersMu.Lock()
	c.state.observers.err = f
	c.state.observersMu.Unlock()
}

// OnReconnectFailed runs when the automatic attempt limit is exhausted.
func (c *Manager) OnReconnectFailed(f func()) {
	c.state.observersMu.Lock()
	c.state.observers.failed = f
	c.state.observersMu.Unlock()
}

// OnPing runs on the engine ping goroutine after a PING write and must not block.
func (c *Manager) OnPing(f func()) {
	c.state.observersMu.Lock()
	c.state.observers.ping = f
	c.state.observersMu.Unlock()
}

// OnPong runs on the engine reader with the PING round trip and must not block.
func (c *Manager) OnPong(f func(latency time.Duration)) {
	c.state.observersMu.Lock()
	c.state.observers.pong = f
	c.state.observersMu.Unlock()
}

func (c *Manager) observerSnapshot() clientObservers {
	c.state.observersMu.RLock()
	defer c.state.observersMu.RUnlock()
	return c.state.observers
}

func (c *Manager) ping() {
	if f := c.observerSnapshot().ping; f != nil {
		f()
	}
}

func (c *Manager) pong(rtt time.Duration) {
	if f := c.observerSnapshot().pong; f != nil {
		f(rtt)
	}
}
