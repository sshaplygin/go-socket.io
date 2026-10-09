// Package framing prototypes the gobwas/ws message layer for the EIO v4 rewrite.
// It is an isolated experiment, not a transport used by the root module.
package framing

import (
	"errors"
	"io"
	"net"
	"sync"
	"unicode/utf8"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

var (
	ErrInvalidLimit = errors.New("framing: message size limit must be positive")
	ErrTooLarge     = errors.New("framing: message too large")
	ErrMessageType  = errors.New("framing: only text and binary messages are supported")
)

// Conn reads and writes complete data messages. One reader and multiple writers
// may run concurrently. Data and control writes share one mutex. Incoming ping,
// pong and close frames are handled by wsutil, including between fragments.
// Protocol/read/write failures close the connection; local WriteMessage argument
// errors leave it open. This prototype does not send a close code on violations.
type Conn struct {
	raw       net.Conn
	reader    *wsutil.Reader
	state     ws.State
	maxBytes  int
	readMu    sync.Mutex
	writeMu   sync.Mutex
	closeOnce sync.Once
	closeErr  error
	total     int64 // Data bytes declared across the current message's fragments.
}

// New wraps an upgraded connection. source may supply buffered bytes left by the
// handshake; nil reads directly from conn. clientSide selects RFC 6455 masking
// rules. maxBytes is a positive data-message limit, excluding control frames.
// The caller sets deadlines on conn; Close interrupts blocked network I/O.
func New(conn net.Conn, source io.Reader, clientSide bool, maxBytes int) (*Conn, error) {
	if maxBytes <= 0 {
		return nil, ErrInvalidLimit
	}
	if source == nil {
		source = conn
	}
	state := ws.StateServerSide
	if clientSide {
		state = ws.StateClientSide
	}
	c := &Conn{raw: conn, state: state, maxBytes: maxBytes}
	c.reader = wsutil.NewReader(source, state)
	c.reader.CheckUTF8 = true
	c.reader.OnIntermediate = c.control
	c.reader.OnContinuation = func(h ws.Header, _ io.Reader) error {
		if h.Length > int64(c.maxBytes)-c.total {
			return ErrTooLarge
		}
		c.total += h.Length
		return nil
	}
	return c, nil
}

// ReadMessage returns a reassembled text or binary message with owned data.
// Fragment headers are checked against the remaining message budget before their
// contents are buffered. Control frames do not consume the data-message budget.
// On error, partial data is discarded and the underlying connection is closed.
func (c *Conn) ReadMessage() (ws.OpCode, []byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for {
		h, err := c.reader.NextFrame()
		if err != nil {
			return 0, nil, c.fail(err)
		}
		if h.OpCode.IsControl() {
			if err := c.control(h, c.reader); err != nil {
				return 0, nil, c.fail(err)
			}
			continue
		}
		if h.Length > int64(c.maxBytes) {
			return 0, nil, c.fail(ErrTooLarge)
		}
		c.total = h.Length
		body, err := io.ReadAll(c.reader)
		if err != nil {
			return 0, nil, c.fail(err)
		}
		return h.OpCode, body, nil
	}
}

// WriteMessage serializes one complete data message. It validates local input
// before writing anything; wsutil.Writer applies endpoint-specific masking.
// The caller must not mutate data until WriteMessage returns.
func (c *Conn) WriteMessage(op ws.OpCode, data []byte) error {
	if op != ws.OpText && op != ws.OpBinary {
		return ErrMessageType
	}
	if len(data) > c.maxBytes {
		return ErrTooLarge
	}
	if op == ws.OpText && !utf8.Valid(data) {
		return wsutil.ErrInvalidUTF8
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	w := wsutil.NewWriter(c.raw, c.state, op)
	if _, err := w.Write(data); err != nil {
		return c.fail(err)
	}
	if err := w.Flush(); err != nil {
		return c.fail(err)
	}
	return nil
}

func (c *Conn) control(h ws.Header, body io.Reader) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := wsutil.ControlFrameHandler(c.raw, c.state)(h, body); err != nil {
		// Keep the write lock until shutdown: a pending writer must not send
		// data after a close reply, including a protocol-error close reply.
		return c.fail(err)
	}
	return nil
}

func (c *Conn) fail(err error) error {
	_ = c.Close()
	return err
}

// Close is idempotent and does not wait for the read or write mutex.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() { c.closeErr = c.raw.Close() })
	return c.closeErr
}
