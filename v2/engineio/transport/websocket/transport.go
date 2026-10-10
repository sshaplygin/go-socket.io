package websocket

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gobwas/ws"

	"github.com/sshaplygin/go-socket.io/engineio/payload"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/utils"
)

// ErrNotHijacker is returned by Accept when the response writer does not
// implement http.Hijacker (HTTP/2, or a wrapper that hides the interface). The
// server answers HTTP 501.
var ErrNotHijacker = errors.New("websocket: response writer does not support hijacking")

// DialError is the error when dialing to a server. It saves Response from
// server.
type DialError struct {
	Response *http.Response

	error
}

// Unwrap returns the dial failure, for errors.Is and errors.As.
func (e DialError) Unwrap() error { return e.error }

// HandshakeError is the error of a handshake the server rejected. Accept has
// already answered the request, so the caller must not write to the response.
type HandshakeError struct {
	Err error
}

func (e HandshakeError) Error() string { return "websocket: handshake: " + e.Err.Error() }

func (e HandshakeError) Unwrap() error { return e.Err }

// Transport is websocket transport. The upgrade hijacks an HTTP/1.1
// connection, and permessage-deflate is never negotiated.
type Transport struct {
	// ReadBufferSize and WriteBufferSize are I/O buffer sizes in bytes. A zero
	// ReadBufferSize reads the socket unbuffered after the handshake, which keeps
	// no memory per idle connection. A zero WriteBufferSize sizes the buffer of
	// each message to hold it in one frame.
	ReadBufferSize  int
	WriteBufferSize int

	// MaxPayload limits one message, in wire bytes (frames of a fragmented
	// message together), on both sides. A larger message ends the connection with
	// close status 1009. Zero means payload.DefaultMaxPayload.
	MaxPayload int

	Subprotocols     []string
	TLSClientConfig  *tls.Config
	HandshakeTimeout time.Duration

	// Proxy returns the HTTP proxy for a dial, or nil for none. Only http proxy
	// URLs are supported, through CONNECT; any other scheme fails the dial.
	Proxy       func(*http.Request) (*url.URL, error)
	NetDial     func(network, addr string) (net.Conn, error)
	CheckOrigin func(r *http.Request) bool
}

// Default is default transport.
var Default = &Transport{}

// Name is the name of websocket transport.
func (t *Transport) Name() string {
	return "websocket"
}

func (t *Transport) maxBytes() int {
	if t.MaxPayload > 0 {
		return t.MaxPayload
	}
	return payload.DefaultMaxPayload
}

// Dial creates a new client connection.
func (t *Transport) Dial(u *url.URL, requestHeader http.Header) (transport.Conn, error) {
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}

	query := u.Query()
	query.Set("transport", t.Name())
	query.Set("t", utils.Timestamp())

	u.RawQuery = query.Encode()

	header := requestHeader.Clone()
	remote := make(http.Header)
	var rejected *http.Response
	d := ws.Dialer{
		ReadBufferSize:  t.ReadBufferSize,
		WriteBufferSize: t.WriteBufferSize,
		Timeout:         t.HandshakeTimeout,
		Protocols:       t.Subprotocols,
		TLSConfig:       t.TLSClientConfig,
		NetDial:         t.netDial(u),
		OnHeader: func(key, value []byte) error {
			remote.Add(string(key), string(value))
			return nil
		},
		OnStatusError: func(_ int, _ []byte, body io.Reader) {
			rejected = readRejection(body)
		},
	}
	if host := header.Get("Host"); host != "" {
		d.Host = host
		header.Del("Host")
	}
	d.Header = ws.HandshakeHeaderHTTP(header)

	raw, br, _, err := d.Dial(context.Background(), u.String())
	if err != nil {
		return nil, DialError{
			error:    err,
			Response: rejected,
		}
	}

	c, err := newConn(raw, br, true, *u, remote, t.maxBytes(), t.ReadBufferSize, t.WriteBufferSize)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	return c, nil
}

// readRejection parses the non-101 answer of a server, keeping at most 4 KiB of
// its body.
func readRejection(r io.Reader) *http.Response {
	resp, err := http.ReadResponse(bufio.NewReader(r), nil)
	if err != nil {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp
}

// Accept accepts a http request and create Conn.
func (t *Transport) Accept(w http.ResponseWriter, r *http.Request) (transport.Conn, error) {
	if _, ok := w.(http.Hijacker); !ok {
		return nil, ErrNotHijacker
	}
	if !t.checkOrigin(r) {
		const msg = "websocket: request origin not allowed by CheckOrigin"
		http.Error(w, msg, http.StatusForbidden)
		return nil, HandshakeError{Err: errors.New(msg)}
	}

	raw, rw, _, err := ws.HTTPUpgrader{Header: w.Header()}.Upgrade(r, w)
	if err != nil {
		// The upgrader answered the request on the hijacked connection.
		if raw != nil {
			_ = raw.Close()
		}
		return nil, HandshakeError{Err: err}
	}

	var br *bufio.Reader
	if rw != nil {
		br = rw.Reader
	}
	c, err := newConn(raw, br, false, *r.URL, r.Header, t.maxBytes(), t.ReadBufferSize, t.WriteBufferSize)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	return c, nil
}

// checkOrigin applies CheckOrigin, or the same-origin rule when it is nil: a
// request without an Origin header passes, otherwise the origin host must equal
// the request host, ignoring case.
func (t *Transport) checkOrigin(r *http.Request) bool {
	if t.CheckOrigin != nil {
		return t.CheckOrigin(r)
	}
	origin := r.Header["Origin"]
	if len(origin) == 0 {
		return true
	}
	u, err := url.Parse(origin[0])
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}
