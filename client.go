package socketio

import (
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
)

// ErrEmptyAddr is returned by NewClient when addr is empty.
var ErrEmptyAddr = client.ErrEmptyAddr

// EmptyAddrErr is the former name of ErrEmptyAddr.
//
// Deprecated: use ErrEmptyAddr.
var EmptyAddrErr = ErrEmptyAddr

// Client is a client for a Socket.IO server.
//
// Deprecated: use client.Client.
type Client struct{ inner client.Client }

// NewClient returns a client configured for addr and opts.
//
// Deprecated: use client.NewClient.
func NewClient(addr string, opts *engineio.Options) (*Client, error) {
	c, err := client.NewClient(addr, opts)
	if err != nil {
		return nil, err
	}
	return &Client{inner: *c}, nil
}

func fmtNS(ns string) string {
	if ns == aliasRootNamespace {
		return rootNamespace
	}
	return ns
}

func (c *Client) Connect() error { return c.inner.Connect() }

// Close closes the connection as Conn.Close does. The drain deadline is the PingTimeout
// of the Options passed to NewClient, not the server's value.
func (c *Client) Close() error                           { return c.inner.Close() }
func (c *Client) Emit(event string, args ...interface{}) { c.inner.Emit(event, args...) }

// OnConnect sets the namespace's connect handler.
func (c *Client) OnConnect(f func(Conn) error) { c.inner.OnConnect(f) }

// OnDisconnect sets the namespace's disconnect handler.
func (c *Client) OnDisconnect(f func(Conn, string)) { c.inner.OnDisconnect(f) }

// OnError sets the namespace's error handler.
func (c *Client) OnError(f func(Conn, error)) { c.inner.OnError(f) }

// OnEvent replaces the namespace's handler for event.
func (c *Client) OnEvent(event string, f interface{}) { c.inner.OnEvent(event, f) }
