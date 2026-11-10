package client

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
	"github.com/sshaplygin/go-socket.io/v2/logger"
)

// Opener is client connection which need receive open message first.
type Opener interface {
	Open() (transport.ConnParameters, error)
}

type client struct {
	conn      transport.Conn
	params    transport.ConnParameters
	transport string
	context   interface{}
	closeOnce sync.Once

	// pingTimer closes the client when no PING arrives within pingInterval +
	// pingTimeout, as the Node client does; each PING restarts it.
	mu        sync.Mutex // guards pingTimer, closed and timedOut
	pingTimer *time.Timer
	closed    bool
	timedOut  bool // pingTimer closed the client
}

// startHeartbeat starts the ping timeout once the OPEN packet is read.
func (c *client) startHeartbeat() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pingTimer = time.AfterFunc(c.params.PingInterval+c.params.PingTimeout, c.pingTimedOut)
}

func (c *client) pingTimedOut() {
	c.mu.Lock()
	c.timedOut = !c.closed
	c.mu.Unlock()
	if err := c.Close(); err != nil {
		logger.Log.Debug("engineio: close connection failed", "err", err)
	}
}

// restartHeartbeat restarts the ping timeout after a PING, unless the client is closed.
func (c *client) restartHeartbeat() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed && c.pingTimer != nil {
		c.pingTimer.Reset(c.params.PingInterval + c.params.PingTimeout)
	}
}

// readErr returns err, the failure of a read, marked as a ping timeout when the
// missing PING closed the client.
func (c *client) readErr(err error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timedOut {
		return fmt.Errorf("ping timeout: %w", err)
	}
	return err
}

func (c *client) SetContext(v interface{}) {
	c.context = v
}

func (c *client) Context() interface{} {
	return c.context
}

func (c *client) ID() string {
	return c.params.SID
}

func (c *client) Transport() string {
	return c.transport
}

func (c *client) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		if c.pingTimer != nil {
			c.pingTimer.Stop()
		}
		c.mu.Unlock()
	})
	return c.conn.Close()
}

// NextReader returns the next MESSAGE and answers each PING read before it with PONG;
// it is the client's only reader. A caller that does not call it for longer than
// pingInterval + pingTimeout, or holds a reader open that long, misses PINGs: the
// client then closes, as on a silent server, and NextReader fails with "ping timeout".
func (c *client) NextReader() (frame.Type, io.ReadCloser, error) {
	for {
		ft, pt, r, err := c.conn.NextReader()
		if err != nil {
			return 0, nil, c.readErr(err)
		}

		switch pt {
		case packet.PING:
			// Answer with PONG, as the Node client does; a failure reaches the reader.
			if err = r.Close(); err != nil {
				logger.Log.Debug("engineio: close reader failed", "err", err)
			}
			c.restartHeartbeat()
			w, err := c.conn.NextWriter(frame.String, packet.PONG)
			if err == nil {
				err = w.Close()
			}
			if err != nil {
				return 0, nil, c.readErr(err)
			}
			continue

		case packet.CLOSE:
			if err = c.Close(); err != nil {
				logger.Log.Debug("engineio: close connection failed", "err", err)
			}

			return 0, nil, io.EOF

		case packet.MESSAGE:
			return ft, r, nil
		}

		// Transports keep a read failure, so the next NextReader returns it: DEBUG.
		if err = r.Close(); err != nil {
			logger.Log.Debug("engineio: close reader failed", "err", err)
		}
	}
}

func (c *client) NextWriter(typ frame.Type) (io.WriteCloser, error) {
	return c.conn.NextWriter(typ, packet.MESSAGE)
}

func (c *client) URL() url.URL {
	return c.conn.URL()
}

func (c *client) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *client) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *client) RemoteHeader() http.Header {
	return c.conn.RemoteHeader()
}
