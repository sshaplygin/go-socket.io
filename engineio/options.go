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

// options holds private server configuration.
type options struct {
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
	// have access to options and keep using logger.Log.
	Logger *slog.Logger
}

func (c *options) getHooks() *Hooks {
	if c != nil {
		return c.Hooks
	}
	return nil
}

func (c *options) getLogger() *slog.Logger {
	if c != nil && c.Logger != nil {
		return logger.Wrap(c.Logger)
	}
	return logger.Log
}

func (c *options) getRequestChecker() CheckerFunc {
	if c != nil && c.RequestChecker != nil {
		return c.RequestChecker
	}
	return defaultChecker
}

func (c *options) getConnInitor() ConnInitorFunc {
	if c != nil && c.ConnInitor != nil {
		return c.ConnInitor
	}
	return defaultInitor
}

func (c *options) getPingTimeout() time.Duration {
	if c != nil && c.PingTimeout != 0 {
		return c.PingTimeout
	}
	return 20 * time.Second
}

func (c *options) getPingInterval() time.Duration {
	if c != nil && c.PingInterval != 0 {
		return c.PingInterval
	}
	return 25 * time.Second
}

func (c *options) getUpgradeTimeout() time.Duration {
	if c != nil && c.UpgradeTimeout > 0 {
		return c.UpgradeTimeout
	}
	return 10 * time.Second
}

func (c *options) getMaxHTTPBufferSize() int64 {
	if c != nil && c.MaxHTTPBufferSize > 0 {
		return c.MaxHTTPBufferSize
	}
	return 1_000_000
}

func (c *options) getTransport() []transport.Transport {
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

func (c *options) getCompressionThreshold() int {
	if c != nil && c.CompressionThreshold > 0 {
		return c.CompressionThreshold
	}
	return 1024
}

func (c *options) getCookie() *http.Cookie {
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

func (c *options) getOriginCheck() func(*http.Request) bool {
	if c != nil && c.DisableCORS {
		return nil
	}
	return allowEveryOrigin
}

func allowEveryOrigin(*http.Request) bool { return true }

func (c *options) getSessionIDGenerator() IDGenerator {
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

// Option configures one instance. Options are applied in order; the last setting wins.
type Option func(*options)

// WithPingTimeout sets the heartbeat reply timeout; zero means 20 seconds.
func WithPingTimeout(value time.Duration) Option {
	return func(config *options) { config.PingTimeout = value }
}

// WithPingInterval sets the heartbeat interval; zero means 25 seconds.
func WithPingInterval(value time.Duration) Option {
	return func(config *options) { config.PingInterval = value }
}

// WithUpgradeTimeout bounds the upgrade probe; nonpositive means 10 seconds.
func WithUpgradeTimeout(value time.Duration) Option {
	return func(config *options) { config.UpgradeTimeout = value }
}

// WithMaxHTTPBufferSize caps a polling POST or websocket message; nonpositive means 1,000,000 bytes.
func WithMaxHTTPBufferSize(value int64) Option {
	return func(config *options) { config.MaxHTTPBufferSize = value }
}

// WithUpgrades enables transport upgrades; the default is true.
func WithUpgrades(value bool) Option {
	return func(config *options) { config.DisableUpgrades = !value }
}

// WithPerMessageDeflate enables websocket compression; the default is false.
func WithPerMessageDeflate(value bool) Option {
	return func(config *options) { config.PerMessageDeflate = value }
}

// WithHTTPCompression enables polling response compression; the default is true.
func WithHTTPCompression(value bool) Option {
	return func(config *options) { config.DisableHTTPCompression = !value }
}

// WithCompressionThreshold sets the compression threshold; nonpositive means 1024 bytes.
func WithCompressionThreshold(value int) Option {
	return func(config *options) { config.CompressionThreshold = value }
}

// WithCookie sets the polling cookie template; nil selects the default io cookie. The template is copied per server.
func WithCookie(value *http.Cookie) Option {
	return func(config *options) { config.Cookie = value }
}

// WithCookieEnabled enables polling session cookies; the default is true.
func WithCookieEnabled(value bool) Option {
	return func(config *options) { config.DisableCookie = !value }
}

// WithCORS enables permissive cross-origin polling and websocket access; the default is true. Explicit transport origin checks take precedence.
func WithCORS(value bool) Option {
	return func(config *options) { config.DisableCORS = !value }
}

// WithSessionIDGenerator sets session ID generation; nil selects cryptographic random IDs.
func WithSessionIDGenerator(value IDGenerator) Option {
	return func(config *options) { config.SessionIDGenerator = value }
}

// WithRequestChecker sets the pre-handshake request check; nil accepts requests.
func WithRequestChecker(value CheckerFunc) Option {
	return func(config *options) { config.RequestChecker = value }
}

// WithConnInitor sets the handshake initializer; nil leaves initialization unchanged.
func WithConnInitor(value ConnInitorFunc) Option {
	return func(config *options) { config.ConnInitor = value }
}

// WithHooks sets synchronous engine observers; nil disables observation. Configure the callbacks before accepting connections.
func WithHooks(value *Hooks) Option {
	return func(config *options) { config.Hooks = value }
}

// WithLogger sets the server and session log sink; nil selects logger.Log.
func WithLogger(value *slog.Logger) Option {
	return func(config *options) { config.Logger = value }
}

// WithTransports sets transports in dial/upgrade order. The slice is copied for each instance.
func WithTransports(values ...transport.Transport) Option {
	return func(config *options) { config.Transports = append(values[:0:0], values...) }
}

func resolveOptions(opts ...Option) *options {
	config := new(options)
	for _, option := range opts {
		if option != nil {
			option(config)
		}
	}
	return config
}
