//go:build !js

package client

import (
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// platformTransport returns the transport that dials for t on this platform: t itself.
func platformTransport(t *websocket.Transport) transport.Transport { return t }
