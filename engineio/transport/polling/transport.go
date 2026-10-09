package polling

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/payload"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// Transport is the transport of polling.
type Transport struct {
	Client      *http.Client
	CheckOrigin func(r *http.Request) bool

	// MaxPayload limits, in wire bytes, one request body this transport reads: a
	// POST on the server, a GET response on the client. On the client it also
	// bounds the bodies of its POSTs until the server advertises its own
	// maxPayload. Zero means payload.DefaultMaxPayload.
	MaxPayload int
}

// Default is the default transport.
var Default = &Transport{
	Client: &http.Client{
		Timeout: time.Minute,
	},
	CheckOrigin: nil,
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

	return dial(client, u, requestHeader, t.MaxPayload)
}

func dial(client *http.Client, url *url.URL, requestHeader http.Header, maxPayload int) (*clientConn, error) {
	if client == nil {
		client = &http.Client{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "", url.String(), nil)
	if err != nil {
		cancel()
		return nil, err
	}
	for k, v := range requestHeader {
		req.Header[k] = v
	}
	// Engine.IO v4 polling bodies are always text; binary packets are base64.
	req.Header.Set("Content-Type", contentType)

	return &clientConn{
		Payload:    payload.New(maxPayload, maxPayload),
		httpClient: client,
		request:    *req,
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}
