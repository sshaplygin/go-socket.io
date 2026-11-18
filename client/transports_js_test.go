//go:build js && wasm

package client

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/browser"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func TestPlatformTransportDefaults(t *testing.T) {
	u, err := url.Parse("http://127.0.0.1/")
	require.NoError(t, err)
	_, _, config := prepareOptions(u, WithTimeout(300*time.Millisecond))
	require.Equal(t, []transport.Transport{polling.Default, &browser.Transport{HandshakeTimeout: 300 * time.Millisecond}}, config.Transports)
}

func TestPlatformTransportReplacesWebsocket(t *testing.T) {
	u, err := url.Parse("http://127.0.0.1/")
	require.NoError(t, err)
	mine := &websocket.Transport{Subprotocols: []string{"chat"}, HandshakeTimeout: time.Second, ReadBufferSize: 7}
	_, _, config := prepareOptions(u, WithTransports(mine), WithPerMessageDeflate(true))
	require.Equal(t, []transport.Transport{&browser.Transport{Subprotocols: []string{"chat"}, HandshakeTimeout: time.Second}}, config.Transports)
	require.Equal(t, &websocket.Transport{Subprotocols: []string{"chat"}, HandshakeTimeout: time.Second, ReadBufferSize: 7}, mine)
}
