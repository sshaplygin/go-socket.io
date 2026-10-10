// Package client is the Engine.IO client: a Dialer that connects to an Engine.IO server
// over the transports it lists and returns the connection as an engineio.Conn.
package client

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/logger"
)

// Dialer is dialer configure.
type Dialer struct {
	Transports []transport.Transport
}

// Dial returns a connection which dials to url with requestHeader.
func (d *Dialer) Dial(urlStr string, requestHeader http.Header) (engineio.Conn, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		logger.Log.Debug("engineio: parse url failed", "err", err)

		return nil, err
	}

	query := u.Query()
	query.Set("EIO", "3")
	u.RawQuery = query.Encode()

	var conn transport.Conn

	for i := len(d.Transports) - 1; i >= 0; i-- {
		if conn != nil {
			if closeErr := conn.Close(); closeErr != nil {
				logger.Log.Debug("engineio: close connection failed", "err", closeErr)
			}
		}

		t := d.Transports[i]

		conn, err = t.Dial(u, requestHeader)
		if err != nil {
			dialFailed(i, t.Name(), err)

			continue
		}

		var params transport.ConnParameters
		if p, ok := conn.(Opener); ok {
			params, err = p.Open()
			if err != nil {
				dialFailed(i, t.Name(), err)

				continue
			}
		} else {
			var pt packet.Type
			var r io.ReadCloser

			_, pt, r, err = conn.NextReader()
			if err != nil {
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
		if err != nil {
			dialFailed(i, t.Name(), err)

			continue
		}

		ret := &client{
			conn:      conn,
			params:    params,
			transport: t.Name(),
			close:     make(chan struct{}),
		}

		go ret.serve()

		return ret, nil
	}

	return nil, err
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
