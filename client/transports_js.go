//go:build js && wasm

package client

import (
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/browser"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// platformTransport returns the transport that dials for t in a browser: the
// browser's WebSocket API with the subprotocols and handshake timeout of t.
func platformTransport(t *websocket.Transport) transport.Transport {
	return &browser.Transport{Subprotocols: t.Subprotocols, HandshakeTimeout: t.HandshakeTimeout}
}
