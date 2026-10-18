package engineio

import (
	"log/slog"
	"net/http"
	"time"

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
	// PerMessageDeflate enables websocket compression; the default is off.
	PerMessageDeflate bool
	// DisableHTTPCompression disables gzip/deflate for polling responses.
	DisableHTTPCompression bool
	// CompressionThreshold is the encoded size from which compression applies;
	// nonpositive values mean 1024 bytes.
	CompressionThreshold int
	// Cookie is copied with Value replaced by the session ID. Nil defaults to
	// io, Path=/, HttpOnly, SameSite=Strict. An empty Name defaults to io.
	Cookie *http.Cookie
	// DisableCookie suppresses the polling handshake transport's session cookie.
	DisableCookie bool

	// DisableCORS restores the legacy CORS behavior. By default every origin is
	// accepted: polling reflects Origin with credentials, or allows * when absent,
	// and websocket upgrades accept cross-origin requests. When disabled, polling
	// sends no CORS headers and websocket uses Gorilla's same-origin check.
	// An explicit transport CheckOrigin overrides either default.
	DisableCORS bool

	Transports         []transport.Transport
	SessionIDGenerator IDGenerator

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
	defaultOriginCheck := c.getOriginCheck()
	resolved := make([]transport.Transport, len(configured))
	for i, t := range configured {
		resolved[i] = t
		switch t := t.(type) {
		case *polling.Transport:
			copied := *t
			if copied.CheckOrigin == nil {
				copied.CheckOrigin = defaultOriginCheck
			}
			copied.Compression = c == nil || !c.DisableHTTPCompression
			copied.CompressionThreshold = c.getCompressionThreshold()
			copied.Cookie = c.getCookie()
			resolved[i] = &copied
		case *websocket.Transport:
			copied := *t
			if copied.CheckOrigin == nil {
				copied.CheckOrigin = defaultOriginCheck
			}
			copied.EnableCompression = c != nil && c.PerMessageDeflate
			copied.CompressionThreshold = c.getCompressionThreshold()
			resolved[i] = &copied
		}
	}
	return resolved
}

func (c *Options) getCompressionThreshold() int {
	if c != nil && c.CompressionThreshold > 0 {
		return c.CompressionThreshold
	}
	return 1024
}

func (c *Options) getCookie() *http.Cookie {
	if c != nil && c.DisableCookie {
		return nil
	}
	cookie := http.Cookie{Name: "io", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode}
	if c != nil && c.Cookie != nil {
		cookie = *c.Cookie
	}
	if cookie.Name == "" {
		cookie.Name = "io"
	}
	return &cookie
}

func (c *Options) getOriginCheck() func(*http.Request) bool {
	if c != nil && c.DisableCORS {
		return nil
	}
	return allowEveryOrigin
}

func allowEveryOrigin(*http.Request) bool { return true }

func (c *Options) getSessionIDGenerator() IDGenerator {
	if c != nil && c.SessionIDGenerator != nil {
		return c.SessionIDGenerator
	}
	return &DefaultIDGenerator{}
}

func defaultChecker(*http.Request) (http.Header, error) {
	return nil, nil
}

func defaultInitor(*http.Request, Conn) {}

// CheckerFunc checks a new handshake. Its headers are included in the response;
// an error rejects the handshake with HTTP 403 and the fixed message Forbidden.
type CheckerFunc func(*http.Request) (http.Header, error)

// ConnInitorFunc initializes a registered session on the handshake request's
// goroutine, with the request live, before OPEN is written and Accept can return
// it. Closing the session here prevents it from reaching Accept.
type ConnInitorFunc func(*http.Request, Conn)
