// Package browser is the websocket transport of a client that runs in a web
// browser (GOOS=js GOARCH=wasm). It opens its connections with the browser's
// WebSocket API instead of a network connection of its own. On other platforms
// the package compiles, and Dial returns an error.
//
// The client package uses this transport on js/wasm in place of each
// websocket.Transport (see client.WithTransports). Wire limits that the browser
// imposes are listed under "Browser clients" in docs/PROTOCOL.md.
package browser

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// Transport is the client websocket transport of a browser. Its fields are read
// on each Dial call and must not be changed while it is in use. The zero value
// is ready to use.
//
// Dial does not send the request headers it is given and its connections
// report no response headers: the browser's WebSocket API cannot set the
// headers of the handshake request or read those of its response. The browser
// sends its own headers, cookies of the server's site included, and negotiates
// compression itself. The wire limits are listed under "Browser clients" in
// docs/PROTOCOL.md.
type Transport struct {
	// Subprotocols are the subprotocols Dial requests from the server.
	Subprotocols []string
	// HandshakeTimeout bounds Dial, from the creation of the WebSocket to its
	// open event; zero means no limit.
	HandshakeTimeout time.Duration
}

// Name returns "websocket", the transport name in the Engine.IO protocol.
func (t *Transport) Name() string {
	return "websocket"
}

// Accept answers the request with HTTP 400 Bad Request and returns an error:
// the transport has no server side.
func (t *Transport) Accept(w http.ResponseWriter, _ *http.Request) (transport.Conn, error) {
	http.Error(w, "the browser transport accepts no connections", http.StatusBadRequest)
	return nil, errors.New("browser: Accept is not supported by a client-only transport")
}

// Dial opens a client websocket connection to u, with the scheme http or https
// changed to ws or wss and the transport and t query parameters set; it
// modifies u. requestHeader is not sent (see Transport). Dial blocks until the
// connection opens, fails or HandshakeTimeout passes, so on js/wasm it must not
// be called from a JavaScript callback, which would block the event loop that
// delivers the open event. A failure on js/wasm is reported without detail,
// because the browser gives none. On other platforms Dial returns an error that
// wraps errors.ErrUnsupported. The connection is safe for one reader and one
// writer at a time.
func (t *Transport) Dial(u *url.URL, _ http.Header) (transport.Conn, error) {
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
	return t.dial(*u)
}
