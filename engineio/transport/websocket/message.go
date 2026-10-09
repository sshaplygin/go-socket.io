package websocket

import (
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// ErrMessageType is returned by a write of anything but a text or binary message.
var ErrMessageType = errors.New("websocket: only text and binary messages are supported")

// closeTimeout bounds how long a failing connection waits for a stuck writer and
// for the peer to accept the close frame it sends before the TCP close.
const closeTimeout = time.Second

// messageConn reads and writes complete WebSocket data messages. One reader and
// multiple writers may run concurrently. Data and control writes share one
// mutex. Incoming ping, pong and close frames are handled by wsutil, including
// between fragments. A protocol, limit or UTF-8 violation by the peer sends a
// close frame with the matching status (1002, 1009, 1007) and closes the
// connection; an I/O failure closes it without a frame. Argument errors of a
// local write leave the connection open.
type messageConn struct {
	raw      net.Conn
	reader   *wsutil.Reader
	state    ws.State
	maxBytes int
	// writeBuf is the wsutil.Writer buffer size; zero sizes it to hold the whole
	// message in one frame.
	writeBuf int

	readMu  sync.Mutex
	writeMu sync.Mutex

	closeOnce sync.Once
	closeErr  error
	closed    atomic.Bool
	closing   atomic.Bool

	total int64 // Data bytes declared across the current message's fragments.
}

// newMessageConn wraps an upgraded connection. source supplies bytes already
// buffered by the handshake, or buffering chosen by the caller; nil reads
// directly from raw. clientSide selects the RFC 6455 masking rules. maxBytes is a
// positive data-message limit, excluding control frames. The caller sets
// deadlines on raw; Close interrupts blocked network I/O.
func newMessageConn(raw net.Conn, source io.Reader, clientSide bool, maxBytes, writeBuf int) (*messageConn, error) {
	if maxBytes <= 0 {
		return nil, ErrInvalidLimit
	}
	if source == nil {
		source = raw
	}
	state := ws.StateServerSide
	if clientSide {
		state = ws.StateClientSide
	}
	c := &messageConn{raw: raw, state: state, maxBytes: maxBytes, writeBuf: writeBuf}
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

// readMessage returns a reassembled text or binary message with owned data.
// Fragment headers are checked against the remaining message budget before their
// contents are buffered. Control frames do not consume the data-message budget.
// On error, partial data is discarded and the connection is closed.
func (c *messageConn) readMessage() (ws.OpCode, []byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if c.closed.Load() || c.closing.Load() {
		return 0, nil, net.ErrClosed
	}
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

// writeMessage sends one complete data message. It validates local input before
// writing anything; wsutil.Writer applies the endpoint's masking. The caller must
// not mutate data until writeMessage returns.
func (c *messageConn) writeMessage(op ws.OpCode, data []byte) error {
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
	if c.closed.Load() || c.closing.Load() {
		return net.ErrClosed
	}
	size := c.writeBuf
	if size <= 0 {
		size = len(data) + ws.MaxHeaderSize
	}
	w := wsutil.GetWriter(c.raw, c.state, op, size)
	defer wsutil.PutWriter(w)
	if _, err := w.Write(data); err != nil {
		return c.fail(err)
	}
	if err := w.Flush(); err != nil {
		return c.fail(err)
	}
	return nil
}

// control handles one control frame. A close frame is answered under the write
// lock, which stays held until shutdown: a pending writer must not send data
// after the close reply.
func (c *messageConn) control(h ws.Header, body io.Reader) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := wsutil.ControlFrameHandler(c.raw, c.state)(h, body); err != nil {
		_ = c.Close()
		return err
	}
	return nil
}

// fail closes the connection because of err and returns err. A violation by the
// peer is reported to it with a close status first.
func (c *messageConn) fail(err error) error {
	c.closeWith(closeStatus(err))
	return err
}

// closeStatus maps a read failure to the close status owed to the peer, or zero
// when none is: I/O errors, a close the peer started and local errors.
func closeStatus(err error) ws.StatusCode {
	var protocol ws.ProtocolError
	switch {
	case errors.Is(err, ErrTooLarge):
		return ws.StatusMessageTooBig
	case errors.Is(err, wsutil.ErrInvalidUTF8):
		return ws.StatusInvalidFramePayloadData
	case errors.As(err, &protocol), errors.Is(err, ws.ErrHeaderLengthMSB),
		errors.Is(err, ws.ErrHeaderLengthUnexpected), errors.Is(err, ErrInvalidPacket):
		return ws.StatusProtocolError
	}
	return 0
}

// closeWith sends a close frame carrying code, unless code is zero, then closes
// the connection. The wait behind a stuck writer and the frame itself are
// bounded by closeTimeout. When the connection can half-close, the frame is
// followed by a FIN and the socket stays open for up to closeTimeout more to
// drain what the peer still sends: closing a TCP connection with unread data
// makes some stacks (Windows) reset it, which can discard the close frame
// before the peer has read it.
func (c *messageConn) closeWith(code ws.StatusCode) {
	if code != 0 && c.closing.CompareAndSwap(false, true) && !c.closed.Load() {
		_ = c.raw.SetWriteDeadline(time.Now().Add(closeTimeout))
		c.writeMu.Lock()
		frame := ws.NewCloseFrame(ws.NewCloseFrameBody(code, ""))
		if c.state.ClientSide() {
			frame = ws.MaskFrameInPlace(frame)
		}
		err := ws.WriteFrame(c.raw, frame)
		var halfClosed bool
		if cw, ok := c.raw.(interface{ CloseWrite() error }); ok && err == nil {
			halfClosed = cw.CloseWrite() == nil
		}
		if !halfClosed {
			_ = c.Close()
		}
		c.writeMu.Unlock()
		if halfClosed {
			go c.drainAndClose()
		}
		return
	}
	_ = c.Close()
}

// drainAndClose discards what the peer still sends, until it closes, closeTimeout
// passes or a megabyte went by, then closes the socket.
func (c *messageConn) drainAndClose() {
	_ = c.raw.SetReadDeadline(time.Now().Add(closeTimeout))
	_, _ = io.CopyN(io.Discard, c.raw, 1<<20)
	_ = c.Close()
}

// Close is idempotent and does not wait for the read or write mutex.
func (c *messageConn) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		c.closeErr = c.raw.Close()
	})
	return c.closeErr
}
