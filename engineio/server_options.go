package engineio

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/sshaplygin/go-socket.io/logger"
)

// Options is options to create a server.
type Options struct {
	PingTimeout  time.Duration
	PingInterval time.Duration

	// UpgradeTimeout bounds the whole upgrade probe. Nonpositive values mean 10 seconds.
	UpgradeTimeout time.Duration
	// MaxHTTPBufferSize caps a polling POST body or websocket message in bytes.
	// Nonpositive values mean 1,000,000; there is no unlimited setting.
	MaxHTTPBufferSize int64
	// DisableUpgrades keeps sessions on their handshake transport.
	DisableUpgrades bool

	// DisableCORS restores the legacy CORS behavior. By default every origin is
	// accepted: polling reflects Origin with credentials, or allows * when absent,
	// and websocket upgrades accept cross-origin requests. When disabled, polling
	// sends no CORS headers and websocket uses Gorilla's same-origin check.
	// An explicit transport CheckOrigin overrides either default.
	DisableCORS bool

	Transports         []transport.Transport
	SessionIDGenerator session.IDGenerator

	RequestChecker CheckerFunc
	ConnInitor     ConnInitorFunc

	// Hooks observes engine events; nil means no observer. Set fields run
	// synchronously and must not call back except to read connection state.
	Hooks *Hooks

	// Logger receives errors and diagnostics from the server and the sessions
	// it creates. nil means logger.Log. It chooses the sink only: logger.Level
	// decides what is enabled while it is not logger.LevelUnset (set from
	// SOCKETIO_LOG_LEVEL or by logger.Level.Set), the handler otherwise.
	// Transports, the parser, the packet codec and the client dialer do not
	// have access to Options and keep using logger.Log.
	Logger *slog.Logger

	// WriteBufferSize is the number of outbound socket.io packets each connection of
	// a socketio.Server or socketio.Client queues; 0 and negative values mean 64. It
	// is unrelated to websocket.Transport.WriteBufferSize, which counts bytes, and
	// the engine.io server ignores it. A packet the writer has started no longer
	// counts. An Emit that finds the queue full closes the connection without
	// draining it (see socketio.ErrWriteBufferFull), so more than WriteBufferSize
	// packets queued faster than the writer sends them can close a healthy client.
	// Polling writes one engine.io frame per poll round trip, and a packet with k
	// binary attachments takes k+1 frames, so polling clients overflow at much lower
	// emit rates than websocket clients. Temporary v1 placement.
	WriteBufferSize int
}

func (c *Options) getHooks() *Hooks {
	if c != nil {
		return c.Hooks
	}
	return nil
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
	return 20 * time.Second
}

func (c *Options) getPingInterval() time.Duration {
	if c != nil && c.PingInterval != 0 {
		return c.PingInterval
	}
	return 25 * time.Second
}

func (c *Options) getUpgradeTimeout() time.Duration {
	if c != nil && c.UpgradeTimeout > 0 {
		return c.UpgradeTimeout
	}
	return 10 * time.Second
}

func (c *Options) getMaxHTTPBufferSize() int64 {
	if c != nil && c.MaxHTTPBufferSize > 0 {
		return c.MaxHTTPBufferSize
	}
	return 1_000_000
}

func (c *Options) getTransport() []transport.Transport {
	configured := []transport.Transport{polling.Default, websocket.Default}
	if c != nil && len(c.Transports) != 0 {
		configured = c.Transports
	}
	if c != nil && c.DisableCORS {
		return configured
	}
	resolved := make([]transport.Transport, len(configured))
	for i, t := range configured {
		resolved[i] = t
		switch t := t.(type) {
		case *polling.Transport:
			if t.CheckOrigin == nil {
				copied := *t
				copied.CheckOrigin = allowEveryOrigin
				resolved[i] = &copied
			}
		case *websocket.Transport:
			if t.CheckOrigin == nil {
				copied := *t
				copied.CheckOrigin = allowEveryOrigin
				resolved[i] = &copied
			}
		}
	}
	return resolved
}

func allowEveryOrigin(*http.Request) bool { return true }

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
