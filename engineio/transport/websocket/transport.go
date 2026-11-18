package websocket

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// DialError is the error Dial returns when the websocket connection cannot be
// opened. Its Error method is that of the underlying gorilla/websocket error.
type DialError struct {
	// Response is the server's HTTP response to a failed websocket handshake,
	// for example to read its status code. It is nil when the failure happened
	// before a response arrived. Its body may be incomplete and need not be
	// closed.
	Response *http.Response

	error
}

// Transport is the websocket transport. Its fields are read on each Dial and
// Accept call and must not be changed while it is in use. An engineio.Server
// uses a copy of a Transport given to engineio.WithTransports, or of Default
// when it is given no transports, with EnableCompression and
// CompressionThreshold set from the server's options and CheckOrigin set to
// the server's origin policy when it is nil.
type Transport struct {
	// EnableCompression negotiates permessage-deflate; frames below the threshold
	// (1024 when nonpositive) are sent uncompressed.
	EnableCompression bool
	// CompressionThreshold is the message size in bytes from which messages are
	// compressed when compression was negotiated; nonpositive means 1024.
	CompressionThreshold int

	// ReadBufferSize is the read buffer size of the connection in bytes; zero
	// selects the gorilla/websocket default. It does not limit message size.
	ReadBufferSize int
	// WriteBufferSize is the write buffer size of the connection in bytes; zero
	// selects the gorilla/websocket default. It does not limit message size.
	WriteBufferSize int

	// Subprotocols are the subprotocols Dial requests from the server. Accept
	// does not negotiate subprotocols.
	Subprotocols []string
	// TLSClientConfig is the TLS configuration Dial uses for wss URLs; nil
	// uses the defaults of crypto/tls.
	TLSClientConfig *tls.Config
	// HandshakeTimeout bounds the connection and the websocket handshake of
	// Dial; zero means no limit. Accept does not use it.
	HandshakeTimeout time.Duration

	// Proxy returns the proxy URL Dial connects through for a request, or a nil
	// URL for a direct connection. A nil Proxy connects directly; set it to
	// http.ProxyFromEnvironment to honor the proxy environment variables.
	Proxy func(*http.Request) (*url.URL, error)
	// NetDial opens the network connection of Dial; nil uses net.Dial.
	NetDial func(network, addr string) (net.Conn, error)
	// CheckOrigin decides whether Accept upgrades a request; a request it
	// rejects is answered with HTTP 403. Nil applies the same-origin check of
	// gorilla/websocket, which accepts requests without an Origin header.
	CheckOrigin func(r *http.Request) bool
}

// Default is the websocket transport with zero settings: no compression, the
// gorilla/websocket buffer sizes, no handshake timeout for Dial and, outside
// an engineio.Server, the same-origin check of gorilla/websocket for Accept.
var Default = &Transport{}

// Name returns "websocket".
func (t *Transport) Name() string {
	return "websocket"
}

// Dial opens a client websocket connection to u, with the scheme http or https
// changed to ws or wss and the transport and t query parameters set; it
// modifies u. A failure is returned as a DialError.
func (t *Transport) Dial(u *url.URL, requestHeader http.Header) (transport.Conn, error) {
	dialer := websocket.Dialer{
		EnableCompression: t.EnableCompression,
		ReadBufferSize:    t.ReadBufferSize,
		WriteBufferSize:   t.WriteBufferSize,
		NetDial:           t.NetDial,
		Proxy:             t.Proxy,
		TLSClientConfig:   t.TLSClientConfig,
		HandshakeTimeout:  t.HandshakeTimeout,
		Subprotocols:      t.Subprotocols,
	}

	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}

	query := u.Query()
	query.Set("transport", t.Name())
	query.Set("t", transport.Timestamp())

	u.RawQuery = query.Encode()
	c, resp, err := dialer.Dial(u.String(), requestHeader)
	if err != nil {
		return nil, DialError{
			error:    err,
			Response: resp,
		}
	}

	return newConn(c, *u, resp.Header, t), nil
}

// Accept upgrades the request to a websocket connection, sending the headers
// already set on w with the upgrade response. When the upgrade fails, the error
// is returned and gorilla/websocket has already answered the request with an
// HTTP error or, for a failure after it took over the connection, closed that
// connection.
func (t *Transport) Accept(w http.ResponseWriter, r *http.Request) (transport.Conn, error) {
	upgrader := websocket.Upgrader{
		EnableCompression: t.EnableCompression,
		ReadBufferSize:    t.ReadBufferSize,
		WriteBufferSize:   t.WriteBufferSize,
		CheckOrigin:       t.CheckOrigin,
	}
	c, err := upgrader.Upgrade(w, r, w.Header())
	if err != nil {
		return nil, err
	}

	return newConn(c, *r.URL, r.Header, t), nil
}
