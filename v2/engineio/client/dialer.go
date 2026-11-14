// Package client is the Engine.IO client: a Dialer that connects to an Engine.IO server
// over the transports it lists and returns the connection as an engineio.Conn.
package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/sshaplygin/go-socket.io/v2/engineio"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/websocket"
	"github.com/sshaplygin/go-socket.io/v2/logger"
)

// defaultHandshakeTimeout is the bound of a zero Dialer.HandshakeTimeout.
const defaultHandshakeTimeout = 10 * time.Second

// Dialer is dialer configure.
type Dialer struct {
	Transports []transport.Transport

	// HandshakeTimeout bounds, for each transport tried, the dial, the websocket
	// handshake and the wait for the OPEN packet; an expired bound fails the attempt
	// with an error that wraps os.ErrDeadlineExceeded. A *websocket.Transport is dialed
	// through a private copy whose HandshakeTimeout is the shorter of the two; the TCP
	// connect of its custom NetDial, which takes no context, is not bounded. For other
	// transports only the wait for OPEN is bounded. Bounds of the transport, such as
	// the timeout of the polling http.Client, apply too. Zero means 10 s; a negative
	// value means no bound.
	HandshakeTimeout time.Duration
}

// Dial returns a connection which dials to url with requestHeader.
func (d *Dialer) Dial(urlStr string, requestHeader http.Header) (engineio.Conn, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		logger.Log.Debug("engineio: parse url failed", "err", err)

		return nil, err
	}

	query := u.Query()
	query.Set("EIO", "4")
	u.RawQuery = query.Encode()

	var conn transport.Conn
	timeout := d.HandshakeTimeout
	if timeout == 0 {
		timeout = defaultHandshakeTimeout
	}

	for i := len(d.Transports) - 1; i >= 0; i-- {
		if conn != nil {
			if closeErr := conn.Close(); closeErr != nil {
				logger.Log.Debug("engineio: close connection failed", "err", closeErr)
			}
		}

		t := d.Transports[i]
		var deadline time.Time
		if timeout > 0 {
			deadline = time.Now().Add(timeout)
		}

		conn, err = withHandshakeTimeout(t, timeout).Dial(u, requestHeader)
		if err != nil {
			err = timedOut(err, deadline)
			dialFailed(i, t.Name(), err)

			continue
		}
		if !deadline.IsZero() {
			err = conn.SetReadDeadline(deadline) // bounds the wait for OPEN
		}

		var params transport.ConnParameters
		if p, ok := conn.(Opener); ok && err == nil {
			params, err = p.Open()
		} else if err == nil {
			var pt packet.Type
			var r io.ReadCloser

			_, pt, r, err = conn.NextReader()
			if err != nil {
				err = timedOut(err, deadline)

				continue
			}

			func() {
				defer func() {
					if closeErr := r.Close(); closeErr != nil {
						logger.Log.Warn("engineio: close reader failed", "err", closeErr)
					}
				}()

				if pt != packet.OPEN {
					err = errors.New("invalid open")

					return
				}

				params, err = transport.ReadConnParameters(r)
				if err != nil {
					return
				}
			}()
		}
		if err == nil && !deadline.IsZero() {
			err = conn.SetReadDeadline(time.Time{})
		}
		if err != nil {
			err = timedOut(err, deadline)
			dialFailed(i, t.Name(), err)

			continue
		}

		ret := &client{
			conn:      conn,
			params:    params,
			transport: t.Name(),
		}

		ret.startHeartbeat()

		return ret, nil
	}

	// The loop closes a failed connection before the next attempt; the last one is
	// closed here.
	if conn != nil {
		if closeErr := conn.Close(); closeErr != nil {
			logger.Log.Debug("engineio: close connection failed", "err", closeErr)
		}
	}

	return nil, err
}

// withHandshakeTimeout returns t, or for a *websocket.Transport a copy whose
// HandshakeTimeout is the shorter of its own, zero meaning none, and timeout.
func withHandshakeTimeout(t transport.Transport, timeout time.Duration) transport.Transport {
	wt, ok := t.(*websocket.Transport)
	if !ok || timeout <= 0 || (wt.HandshakeTimeout > 0 && wt.HandshakeTimeout <= timeout) {
		return t
	}
	c := *wt
	c.HandshakeTimeout = timeout
	return &c
}

// timedOut returns err of an attempt whose deadline has passed as an error that wraps
// os.ErrDeadlineExceeded; other errors are returned unchanged.
func timedOut(err error, deadline time.Time) error {
	if deadline.IsZero() || time.Now().Before(deadline) || errors.Is(err, os.ErrDeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w: %w", os.ErrDeadlineExceeded, err)
}

// dialFailed logs a failed attempt with transport name: at DEBUG for the last
// transport tried (i == 0), whose error Dial returns, and at WARN for the others,
// whose errors no caller receives.
func dialFailed(i int, name string, err error) {
	level := slog.LevelWarn
	if i == 0 {
		level = slog.LevelDebug
	}
	logger.Log.Log(context.Background(), level, "engineio: transport dial failed", "transport", name, "err", err)
}
