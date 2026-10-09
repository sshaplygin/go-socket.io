package client

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/payload"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/logger"
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
	close     chan struct{}
	closeOnce sync.Once
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
		close(c.close)
	})
	return c.conn.Close()
}

func (c *client) NextReader() (frame.Type, io.ReadCloser, error) {
	for {
		ft, pt, r, err := c.conn.NextReader()
		if err != nil {
			return 0, nil, err
		}

		switch pt {
		case packet.PONG:
			if err = c.conn.SetReadDeadline(time.Now().Add(c.params.PingInterval + c.params.PingTimeout)); err != nil {
				return 0, nil, err
			}

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

func (c *client) serve() {
	defer func() {
		if closeErr := c.conn.Close(); closeErr != nil {
			logger.Log.Debug("engineio: close connection failed", "err", closeErr)
		}
	}()

	for {
		select {
		case <-c.close:
			return
		case <-time.After(c.params.PingInterval):
		}

		w, err := c.conn.NextWriter(frame.String, packet.PING)
		if err != nil {
			c.pingFailed(err)

			return
		}

		if err = w.Close(); err != nil {
			c.pingFailed(err)

			return
		}

		if err = c.conn.SetWriteDeadline(time.Now().Add(c.params.PingInterval + c.params.PingTimeout)); err != nil {
			c.pingFailed(err)
		}
	}
}

// pingFailed logs a failure of the ping loop. By the 1.L Levels rule it is DEBUG when
// it is expected closure: a Close the client started, io.EOF or a closed connection, a
// *net.OpError (a peer close, reset or passed deadline), a websocket close frame from
// the peer (websocket.ErrCloseSent after the reply to it, or a *websocket.CloseError),
// or a polling *payload.OpError that is not temporary (the transport stores a request
// failure and closes itself before a writer sees it, or the deadline passed). It is WARN
// otherwise: no caller receives it.
func (c *client) pingFailed(err error) {
	level := slog.LevelWarn
	var netErr *net.OpError
	var payloadErr *payload.OpError
	var closeErr *websocket.CloseError
	select {
	case <-c.close:
		level = slog.LevelDebug
	default:
		if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.As(err, &netErr) ||
			errors.Is(err, websocket.ErrCloseSent) || errors.As(err, &closeErr) ||
			(errors.As(err, &payloadErr) && !payloadErr.Temporary()) {
			level = slog.LevelDebug
		}
	}
	logger.Log.Log(context.Background(), level, "engineio: ping failed", "err", err)
}
