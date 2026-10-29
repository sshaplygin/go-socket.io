package client

import (
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/sshaplygin/go-socket.io/logger"
)

// Client multiplexes namespace sockets over one Engine.IO connection.
type Client struct{ manager *manager }

// New configures addr (http://host:port[/namespace]) without opening a connection.
// The address path selects the namespace of the Client's own event methods.
func New(addr string, opts ...Option) (*Client, error) {
	if addr == "" {
		return nil, ErrEmptyAddr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	address, namespace, config := prepareOptions(u, opts...)
	return &Client{manager: newManager(address, namespace, config, loggerFrom(&config), nil)}, nil
}

// Connect returns once Engine.IO is open and namespace CONNECT packets are queued.
// A failed or timed-out attempt can be retried; late dial results are closed.
func (c *Client) Connect() error { return c.manager.Connect() }

// Close disconnects all namespace sockets and drains their queued packets before
// closing Engine.IO, bounded by WithCloseTimeout.
func (c *Client) Close() error { return c.manager.Close() }

// Emit sends through the namespace selected by the address passed to New.
func (c *Client) Emit(event string, args ...interface{}) { c.manager.Emit(event, args...) }

// OnConnect sets the default namespace's connection callback.
func (c *Client) OnConnect(f func(Conn) error) { c.manager.OnConnect(f) }

// OnDisconnect sets the default namespace's disconnect callback.
func (c *Client) OnDisconnect(f func(Conn, string)) { c.manager.OnDisconnect(f) }

// OnError sets the default namespace's error callback.
func (c *Client) OnError(f func(Conn, error)) { c.manager.OnError(f) }

// OnEvent replaces the default namespace's handler for event.
func (c *Client) OnEvent(event string, f interface{}) { c.manager.OnEvent(event, f) }

// OnReconnectAttempt sets the callback before each automatic dial; nil removes it.
func (c *Client) OnReconnectAttempt(f func(attempt int)) { c.manager.OnReconnectAttempt(f) }

// OnReconnect runs after the engine opens, before namespace CONNECT packets are sent.
func (c *Client) OnReconnect(f func(attempt int)) { c.manager.OnReconnect(f) }

// OnReconnectError sets the callback for a failed automatic dial; nil removes it.
func (c *Client) OnReconnectError(f func(err error)) { c.manager.OnReconnectError(f) }

// OnReconnectFailed runs when the automatic attempt limit is exhausted.
func (c *Client) OnReconnectFailed(f func()) { c.manager.OnReconnectFailed(f) }

// OnPing runs on the engine ping goroutine after a PING write and must not block.
func (c *Client) OnPing(f func()) { c.manager.OnPing(f) }

// OnPong runs on the engine reader with the PING round trip and must not block.
func (c *Client) OnPong(f func(latency time.Duration)) { c.manager.OnPong(f) }

// Socket returns the namespace socket, creating it. A namespace query is carried
// by its CONNECT packet; a socket added after Connect starts joining immediately.
func (c *Client) Socket(nsp string) *Socket { return c.manager.Socket(nsp) }

// loggerFrom chooses this client's configured logger or the package fallback.
func loggerFrom(opts *options) *slog.Logger {
	if opts != nil && opts.Logger != nil {
		return logger.Wrap(opts.Logger)
	}
	return logger.Log
}

// ErrEmptyAddr is returned by New when addr is empty.
var ErrEmptyAddr = errors.New("client: empty addr")

// prepareOptions resolves defaults and copies mutable inputs for this instance.
func prepareOptions(u *url.URL, opts ...Option) (address, namespace string, config options) {
	config = *resolveOptions(opts...)
	if config.Timeout == 0 {
		config.Timeout = 20 * time.Second
	}
	if config.ReconnectionDelay <= 0 {
		config.ReconnectionDelay = time.Second
	}
	if config.ReconnectionDelayMax <= 0 {
		config.ReconnectionDelayMax = 5 * time.Second
	}
	if config.RandomizationFactor == 0 {
		config.RandomizationFactor = 0.5
	}
	if config.Path == "" {
		config.Path = "/socket.io"
	}
	if config.Transports == nil {
		handshake := config.Timeout
		if handshake < 0 {
			handshake = 0
		}
		config.Transports = []transport.Transport{polling.Default, &websocket.Transport{HandshakeTimeout: handshake}}
	} else {
		config.Transports = append([]transport.Transport(nil), config.Transports...)
	}
	for i, configured := range config.Transports {
		if t, ok := configured.(*websocket.Transport); ok {
			copied := *t
			copied.EnableCompression = config.PerMessageDeflate
			copied.CompressionThreshold = 1024
			config.Transports[i] = &copied
		}
	}
	if config.WriteBufferSize <= 0 {
		config.WriteBufferSize = defaultWriteBufferSize
	}
	if config.CloseTimeout <= 0 {
		config.CloseTimeout = defaultDrainTimeout
	}
	config.Header = config.Header.Clone()
	query := u.Query()
	for key, values := range config.Query {
		query[key] = append([]string(nil), values...)
	}
	config.Query = query
	namespace = u.Path
	if namespace == "/" {
		namespace = ""
	}
	u.Path, u.RawPath, u.RawQuery = strings.TrimRight(config.Path, "/")+"/", "", query.Encode()

	return u.String(), namespace, config
}
