package polling

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// Transport is the transport of polling.
type Transport struct {
	// Client controls HTTP timeouts and connection pooling. Nil uses Default.Client.
	// For custom limits, use client.WithTransports(&polling.Transport{Client: c}).
	Client      *http.Client
	CheckOrigin func(r *http.Request) bool
	// Compression enables gzip/deflate responses at CompressionThreshold bytes
	// (1024 when nonpositive). Cookie is copied with the session ID as its value.
	Compression          bool
	CompressionThreshold int
	Cookie               *http.Cookie
}

// Default is the default transport. Its Client times out requests after one
// minute. When http.DefaultTransport is an *http.Transport at initialization,
// Client uses a clone of it that keeps up to 64 idle connections per host,
// shared by every client that dials through Default.
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

// Name is the name of transport.
func (t *Transport) Name() string {
	return "polling"
}

// Accept accepts a http request and create Conn.
func (t *Transport) Accept(w http.ResponseWriter, r *http.Request) (transport.Conn, error) {
	conn := newServerConn(t, r)
	return conn, nil
}

// Dial dials connection to url.
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
