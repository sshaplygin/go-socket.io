package client

import (
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/sshaplygin/go-socket.io/logger"
	"github.com/sshaplygin/go-socket.io/parser"
)

// ErrEmptyAddr is returned by New when addr is empty.
var ErrEmptyAddr = errors.New("empty addr")

// ErrConnectTimeout means the engine connection did not open within Options.Timeout.
var ErrConnectTimeout = errors.New("client: connect timeout")

// Client multiplexes namespace sockets over one Engine.IO connection.
type Client struct {
	namespace string
	url       string
	conn      *conn // the current connection; retained for internal queue tests
	handlers  *namespaceHandlers
	limits    connLimits
	log       *slog.Logger
	options   Options
	state     *clientState
	dial      func(string) (engineclient.Conn, error) // internal transport fixture
}

// New configures addr (http://host:port[/namespace]) without opening a connection.
// The address path selects the namespace of the Client's own event methods.
func New(addr string, opts *Options) (*Client, error) {
	if addr == "" {
		return nil, ErrEmptyAddr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	var options Options
	if opts != nil {
		options = *opts
	}
	if options.Timeout == 0 {
		options.Timeout = 20 * time.Second
	}
	if options.ReconnectionDelay <= 0 {
		options.ReconnectionDelay = time.Second
	}
	if options.ReconnectionDelayMax <= 0 {
		options.ReconnectionDelayMax = 5 * time.Second
	}
	if options.RandomizationFactor == 0 {
		options.RandomizationFactor = 0.5
	}
	if options.Path == "" {
		options.Path = "/socket.io"
	}
	if options.Transports == nil {
		handshake := options.Timeout
		if handshake < 0 {
			handshake = 0
		}
		options.Transports = []transport.Transport{polling.Default, &websocket.Transport{HandshakeTimeout: handshake}}
	} else {
		options.Transports = append([]transport.Transport(nil), options.Transports...)
	}
	for i, configured := range options.Transports {
		if t, ok := configured.(*websocket.Transport); ok {
			copied := *t
			copied.EnableCompression = options.PerMessageDeflate
			copied.CompressionThreshold = 1024
			options.Transports[i] = &copied
		}
	}
	options.Header = options.Header.Clone()
	query := u.Query()
	for key, values := range options.Query {
		query[key] = append([]string(nil), values...)
	}
	options.Query = query
	namespace := fmtNS(u.Path)
	u.Path, u.RawPath, u.RawQuery = strings.TrimRight(options.Path, "/")+"/", "", query.Encode()

	c := &Client{namespace: namespace, url: u.String(), handlers: newNamespaceHandlers(),
		limits: newConnLimits(&options), log: loggerFrom(&options), options: options}
	c.state = &clientState{client: c, sockets: make(map[string]*Socket)}
	c.createNamespace(rootNamespace)
	c.Socket(namespace)
	return c, nil
}

// Connect returns once Engine.IO is open and namespace CONNECT packets are queued.
// A failed or timed-out attempt can be retried; late dial results are closed.
func (c *Client) Connect() error { return c.state.connect(c) }

// Close disconnects all namespace sockets and drains their queued packets before
// closing Engine.IO, bounded by Options.CloseTimeout.
func (c *Client) Close() error { return c.state.closeAll() }

// Emit sends through the namespace selected by the address passed to New.
func (c *Client) Emit(event string, args ...interface{}) { c.Socket(c.namespace).Emit(event, args...) }

// OnConnect sets the default namespace's connection callback.
func (c *Client) OnConnect(f func(Conn) error) { c.Socket(c.namespace).OnConnect(f) }

// OnDisconnect sets the default namespace's disconnect callback.
func (c *Client) OnDisconnect(f func(Conn, string)) { c.Socket(c.namespace).OnDisconnect(f) }

// OnError sets the default namespace's error callback.
func (c *Client) OnError(f func(Conn, error)) { c.Socket(c.namespace).OnError(f) }

// OnEvent replaces the default namespace's handler for event.
func (c *Client) OnEvent(event string, f interface{}) { c.Socket(c.namespace).OnEvent(event, f) }

func (c *Client) createNamespace(ns string) *namespaceHandler {
	if h := c.getNamespace(ns); h != nil {
		return h
	}
	h := newNamespaceHandler(ns)
	c.handlers.Set(ns, h)
	return h
}

func (c *Client) getNamespace(ns string) *namespaceHandler {
	h, _ := c.handlers.Get(ns)
	return h
}

func (c *conn) connectClient() error {
	rootHandler, ok := c.handlers.Get(rootNamespace)
	if !ok {
		return errUnavailableRootHandler
	}
	root := newNamespaceConn(c, aliasRootNamespace, rootHandler.broadcast)
	c.namespaces.Set(rootNamespace, root)
	root.Join(c.Conn.ID())
	root.SetContext(c.Conn.Context())
	return nil
}

func (c *Client) engineDial() (engineclient.Conn, error) {
	if c.dial != nil {
		return c.dial(c.url)
	}
	d := engineclient.Dialer{Transports: c.options.Transports, Upgrade: true, OnPing: c.ping, OnPong: c.pong}
	return d.Dial(c.url, c.options.Header)
}

// loggerFrom chooses this client's configured logger or the package fallback.
func loggerFrom(opts *Options) *slog.Logger {
	if opts != nil && opts.Logger != nil {
		return logger.Wrap(opts.Logger)
	}
	return logger.Log
}

func socketConnectHeader(s *Socket) parser.Header {
	nsp := s.namespace
	if s.query != "" {
		nsp += "?" + s.query
	}
	return parser.Header{Type: parser.Connect, Namespace: nsp}
}
