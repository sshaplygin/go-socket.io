package polling

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// Transport is the HTTP long-polling transport. Its fields must not be changed
// while it is in use. An engineio.Server uses a copy of a Transport given to
// engineio.WithTransports, with Compression, CompressionThreshold and Cookie
// set from the server's options and CheckOrigin set to the server's origin
// policy when it is nil.
type Transport struct {
	// Client controls HTTP timeouts and connection pooling. Nil uses Default.Client.
	// For custom limits, use client.WithTransports(&polling.Transport{Client: c}).
	Client *http.Client
	// CheckOrigin decides whether a server response carries CORS headers:
	// when it returns true, Access-Control-Allow-Origin reflects the request's
	// Origin together with Access-Control-Allow-Credentials, or is * when the
	// request has no Origin. It does not reject requests. Nil uses
	// Default.CheckOrigin, which is nil: no CORS headers.
	CheckOrigin func(r *http.Request) bool
	// Compression enables gzip/deflate responses at CompressionThreshold bytes
	// (1024 when nonpositive). Cookie is copied with the session ID as its value.
	Compression bool
	// CompressionThreshold is the response size in bytes from which a server
	// response is compressed when Compression is set; nonpositive means 1024.
	CompressionThreshold int
	// Cookie, when not nil, is set on the responses of a server session that
	// opened on polling, from its handshake on, with Value replaced by the
	// session ID.
	Cookie *http.Cookie
}

// Default is the default transport. Its Client times out requests after one
// minute. When http.DefaultTransport is an *http.Transport at initialization,
// Client uses a clone of it that keeps up to 64 idle connections per host,
// shared by every client that dials through Default. Later changes to
// http.DefaultTransport do not affect Default.
var Default = &Transport{
	Client: &http.Client{
		Timeout:   time.Minute,
		Transport: defaultHTTPTransport(),
	},
	CheckOrigin: nil,
}

// defaultHTTPTransport clones a standard http.DefaultTransport so polling
// clients share a pool without mutating the application's default. A custom
// RoundTripper keeps its own pool settings.
func defaultHTTPTransport() http.RoundTripper {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok || base == nil {
		return http.DefaultTransport
	}
	t := base.Clone()
	t.MaxIdleConnsPerHost = 64
	return t
}

// Name returns "polling".
func (t *Transport) Name() string {
	return "polling"
}

// Accept returns the server connection of a session that starts with the
// request r; it never fails and writes nothing to w. The connection serves the
// later requests of the session through its ServeHTTP method.
func (t *Transport) Accept(w http.ResponseWriter, r *http.Request) (transport.Conn, error) {
	conn := newServerConn(t, r)
	return conn, nil
}

// Dial returns a client connection to u, with the transport query parameter
// set; it modifies u. It sends no request: the connection's Open performs the
// handshake. Requests use t.Client, or Default.Client when it is nil.
func (t *Transport) Dial(u *url.URL, requestHeader http.Header) (transport.Conn, error) {
	query := u.Query()
	query.Set("transport", t.Name())
	u.RawQuery = query.Encode()

	client := t.Client
	if client == nil {
		client = Default.Client
	}

	return dial(client, u, requestHeader)
}

func dial(client *http.Client, url *url.URL, requestHeader http.Header) (*clientConn, error) {
	if client == nil {
		client = &http.Client{}
	}
	req, err := http.NewRequest("", url.String(), nil)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	for k, v := range requestHeader {
		req.Header[k] = v
	}
	supportBinary := req.URL.Query().Get("b64") == ""
	if supportBinary {
		req.Header.Set("Content-Type", "application/octet-stream")
	} else {
		req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	}

	return &clientConn{
		payload:    newPayload(supportBinary),
		httpClient: client,
		request:    *req,
		cancel:     cancel,
	}, nil
}
