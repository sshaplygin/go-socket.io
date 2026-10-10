package engineio

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/sshaplygin/go-socket.io/v2/engineio/session"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/websocket"
	"github.com/sshaplygin/go-socket.io/v2/logger"
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
	// it creates. nil means logger.Log. It chooses the sink only: logger.Level
	// decides what is enabled while it is not logger.LevelUnset (set from
	// SOCKETIO_LOG_LEVEL or by logger.Level.Set), the handler otherwise.
	// Transports, the parser, the packet codec and the client dialer do not
	// have access to Options and keep using logger.Log.
	Logger *slog.Logger

	// WriteBufferSize was the number of outbound socket.io packets each connection of
	// the v1 socketio.Server or socketio.Client queued (0 and negative meant 64). It
	// is unrelated to websocket.Transport.WriteBufferSize, which counts bytes, and the
	// engine.io server ignores it. Nothing reads it since stage 2.0 removed the v1 root
	// runtime; socketio.Options.OutboundQueueGroups replaces it. Temporary v1 placement.
	WriteBufferSize int

	// Hooks are the observer callbacks. nil means none. They are declared but not
	// fired yet (roadmap 2.4E).
	Hooks *Hooks

	// PayloadPreviewBytes is the opt-in size of PacketInfo.Preview: 0 disables
	// capture and the accepted range is 0 to 256. Nothing is captured until stage
	// 2.4E, which also adds the redaction boundary a preview needs.
	PayloadPreviewBytes int
}

// Normalize validates the observer fields of a copy of the options. It does not
// invoke user callbacks, resolve loggers or change the application default logger.
func (c Options) Normalize() (Options, error) {
	if c.PayloadPreviewBytes < 0 || c.PayloadPreviewBytes > 256 {
		return Options{}, fmt.Errorf("engineio: PayloadPreviewBytes %d outside 0..256", c.PayloadPreviewBytes)
	}
	return c, nil
}

// CheckerFunc is function to check request.
type CheckerFunc func(*http.Request) (http.Header, error)

// ConnInitorFunc is function to do after create connection.
type ConnInitorFunc func(*http.Request, Conn)

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
