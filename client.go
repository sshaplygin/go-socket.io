package socketio

import (
	"errors"
	"log/slog"
	"net/url"
	"path"
	"strings"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/parser"
)

// ErrEmptyAddr is returned by NewClient when addr is empty.
var ErrEmptyAddr = errors.New("empty addr")

// EmptyAddrErr is the former name of ErrEmptyAddr.
//
// Deprecated: use ErrEmptyAddr.
var EmptyAddrErr = ErrEmptyAddr

// Client is client for socket.io server
type Client struct {
	namespace string
	url       string

	conn     *conn
	handlers *namespaceHandlers

	opts   *engineio.Options
	limits connLimits
	log    *slog.Logger

	dial func(url string) (engineio.Conn, error) // nil dials over polling; tests replace it
}

// NewClient returns a server
// addr like http://asd.com:8080/{$namespace}
func NewClient(addr string, opts *engineio.Options) (*Client, error) {
	if addr == "" {
		return nil, ErrEmptyAddr
	}

	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}

	namespace := fmtNS(u.Path)

	// Not allowing other than default
	u.Path = path.Join("/socket.io", namespace)
	u.Path = u.EscapedPath()
	if strings.HasSuffix(u.Path, "socket.io") {
		u.Path += "/"
	}

	return &Client{
		namespace: namespace,
		url:       u.String(),
		handlers:  newNamespaceHandlers(),
		opts:      opts,
		limits:    newConnLimits(opts),
		log:       loggerFrom(opts),
	}, nil
}

func fmtNS(ns string) string {
	if ns == aliasRootNamespace {
		return rootNamespace
	}

	return ns
}

func (c *Client) Connect() error {
	dialer := engineio.Dialer{
		Transports: []transport.Transport{polling.Default},
	}

	dial := c.dial
	if dial == nil {
		dial = func(url string) (engineio.Conn, error) { return dialer.Dial(url, nil) }
	}
	enginioCon, err := dial(c.url)
	if err != nil {
		return err
	}

	c.conn = newConn(enginioCon, c.handlers, c.limits, c.log.With("sid", enginioCon.ID()))

	if err := c.conn.connectClient(); !c.conn.connected(err) {
		return err
	}

	go c.conn.serveError()
	go c.conn.serveWrite()
	go c.conn.serveRead(clientConnectPacketHandler, clientDisconnectPacketHandler)

	return nil
}

// Close closes the connection as Conn.Close does. The drain deadline is the PingTimeout
// of the Options passed to NewClient, not the server's value.
func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) Emit(event string, args ...interface{}) {
	nsConn, ok := c.conn.namespaces.Get(c.namespace)
	if !ok && isDone(c.conn.closing) { // a close took it; write queues or drops by the close rules
		nsConn, ok = newNamespaceConn(c.conn, c.namespace, nil), true
	}
	if !ok {
		c.log.Info("emit before namespace connected", "namespace", c.namespace, "event", event)
		return
	}

	nsConn.Emit(event, args...)
}

// OnConnect set a handler function f to handle open event for namespace.
func (c *Client) OnConnect(f func(Conn) error) {
	h := c.getNamespace(c.namespace)
	if h == nil {
		h = c.createNamespace(c.namespace)
	}

	h.OnConnect(f)
}

// OnDisconnect set a handler function f to handle disconnect event for namespace.
func (c *Client) OnDisconnect(f func(Conn, string)) {
	h := c.getNamespace(c.namespace)
	if h == nil {
		h = c.createNamespace(c.namespace)
	}

	h.OnDisconnect(f)
}

// OnError set a handler function f to handle error for namespace.
func (c *Client) OnError(f func(Conn, error)) {
	h := c.getNamespace(c.namespace)
	if h == nil {
		h = c.createNamespace(c.namespace)
	}

	h.OnError(f)
}

// OnEvent set a handler function f to handle event for namespace.
func (c *Client) OnEvent(event string, f interface{}) {
	h := c.getNamespace(c.namespace)
	if h == nil {
		h = c.createNamespace(c.namespace)
	}

	h.OnEvent(event, f)
}

func (c *Client) createNamespace(ns string) *namespaceHandler {
	handler := newNamespaceHandler(ns, nil)
	c.handlers.Set(ns, handler)

	return handler
}

func (c *Client) getNamespace(ns string) *namespaceHandler {
	ret, ok := c.handlers.Get(ns)
	if !ok {
		return nil
	}

	return ret
}

func (c *conn) connectClient() error {
	rootHandler, ok := c.handlers.Get(rootNamespace)
	if !ok {
		return errUnavailableRootHandler
	}

	root := newNamespaceConn(c, aliasRootNamespace, rootHandler.broadcast)
	c.namespaces.Set(rootNamespace, root)

	root.Join(root.Conn.ID())

	c.namespaces.Range(func(ns string, nc *namespaceConn) {
		nc.SetContext(c.Conn.Context())
	})

	header := parser.Header{
		Type: parser.Connect,
	}

	return c.encoder.Encode(header)
}
