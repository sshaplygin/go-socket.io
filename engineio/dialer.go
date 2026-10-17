package engineio

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/logger"
)

// Dialer is dialer configure.
type Dialer struct {
	Transports []transport.Transport
	// Upgrade opens the first transport and probes advertised later transports
	// asynchronously. False preserves last-first fallback without probing.
	Upgrade bool
	// UpgradeTimeout bounds a probe from Dial's return through writing UPGRADE.
	// Nonpositive values mean ten seconds; the OPEN heartbeat window clamps it.
	// A transport's Dial has its own timeout; a late connection is closed.
	UpgradeTimeout time.Duration
	// OnPing runs after a PING write on the ping goroutine; OnPong runs on the
	// NextReader goroutine with the time since that PING. Observers must not block.
	OnPing func()
	OnPong func(rtt time.Duration)
}

// Dial returns a connection which dials to url with requestHeader.
func (d *Dialer) Dial(urlStr string, requestHeader http.Header) (Conn, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		logger.Log.Debug("engineio: parse url failed", "err", err)

		return nil, err
	}

	query := u.Query()
	query.Set("EIO", "3")
	u.RawQuery = query.Encode()

	var conn transport.Conn
	transports := append([]transport.Transport(nil), d.Transports...)
	if len(transports) == 0 {
		return nil, errors.New("engineio: no transports")
	}
	order := make([]int, 0, len(transports))
	if d.Upgrade {
		order = append(order, 0)
	} else {
		for i := len(transports) - 1; i >= 0; i-- {
			order = append(order, i)
		}
	}

	for _, i := range order {
		if conn != nil {
			if closeErr := conn.Close(); closeErr != nil {
				logger.Log.Debug("engineio: close connection failed", "err", closeErr)
			}
		}

		t := transports[i]
		attemptURL := *u

		conn, err = t.Dial(&attemptURL, requestHeader)
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
						if err == nil {
							err = closeErr
						}
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
			onPing:    d.OnPing,
			onPong:    d.OnPong,
		}
		started := time.Now()
		ret.initialize()
		go ret.serve()
		if d.Upgrade && len(transports) > 1 {
			go ret.upgradeTransports(started, *u, requestHeader.Clone(), transports[1:], d.UpgradeTimeout)
		}

		return ret, nil
	}
	if conn != nil {
		_ = conn.Close()
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
