package engineio

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/googollee/go-socket.io/engineio/session"
	"github.com/googollee/go-socket.io/engineio/transport"
	"github.com/googollee/go-socket.io/engineio/transport/polling"
	"github.com/googollee/go-socket.io/engineio/transport/websocket"
	"github.com/googollee/go-socket.io/logger"
)

// Options is options to create a server.
type Options struct {
	PingTimeout  time.Duration
	PingInterval time.Duration

	Transports         []transport.Transport
	SessionIDGenerator session.IDGenerator

	RequestChecker CheckerFunc
	ConnInitor     ConnInitorFunc

	// Logger receives errors and diagnostics from the server and the sessions
	// it creates. nil means logger.Log. It chooses the sink only: the level is
	// the handler's, or SOCKETIO_LOG_LEVEL when set (see package logger).
	// Transports, the parser, the packet codec and the client dialer do not
	// have access to Options and keep using logger.Log.
	Logger *slog.Logger
}

func (c *Options) getLogger() *slog.Logger {
	if c != nil && c.Logger != nil {
		return logger.Wrap(c.Logger)
	}
	return logger.Log
}

func (c *Options) getRequestChecker() CheckerFunc {
	if c != nil && c.RequestChecker != nil {
		return c.RequestChecker
	}
	return defaultChecker
}

func (c *Options) getConnInitor() ConnInitorFunc {
	if c != nil && c.ConnInitor != nil {
		return c.ConnInitor
	}
	return defaultInitor
}

func (c *Options) getPingTimeout() time.Duration {
	if c != nil && c.PingTimeout != 0 {
		return c.PingTimeout
	}
	return time.Minute
}

func (c *Options) getPingInterval() time.Duration {
	if c != nil && c.PingInterval != 0 {
		return c.PingInterval
	}
	return time.Second * 20
}

func (c *Options) getTransport() []transport.Transport {
	if c != nil && len(c.Transports) != 0 {
		return c.Transports
	}
	return []transport.Transport{
		polling.Default,
		websocket.Default,
	}
}

func (c *Options) getSessionIDGenerator() session.IDGenerator {
	if c != nil && c.SessionIDGenerator != nil {
		return c.SessionIDGenerator
	}
	return &session.DefaultIDGenerator{}
}

func defaultChecker(*http.Request) (http.Header, error) {
	return nil, nil
}

func defaultInitor(*http.Request, Conn) {}
