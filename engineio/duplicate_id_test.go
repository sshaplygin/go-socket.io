package engineio_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/stretchr/testify/require"
)

type constantSessionID struct{}

func (constantSessionID) NewID() string { return "duplicate" }

func TestDuplicateSessionIDKeepsFirstSession(t *testing.T) {
	server := engineio.NewServer(engineio.WithSessionIDGenerator(constantSessionID{}))
	ts := httptest.NewServer(server)
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = server.Close() })
	target := "ws" + strings.TrimPrefix(ts.URL, "http") + "/?EIO=3&transport=websocket"
	first, _, err := websocket.DefaultDialer.Dial(target, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	require.NoError(t, first.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err = first.ReadMessage()
	require.NoError(t, err)
	_, err = server.Accept()
	require.NoError(t, err)
	second, _, err := websocket.DefaultDialer.Dial(target, nil)
	// HTTP has already upgraded when the session ID is generated, so rejection
	// appears as a closed websocket before its Engine.IO OPEN packet.
	if err == nil {
		t.Cleanup(func() { _ = second.Close() })
		require.NoError(t, second.SetReadDeadline(time.Now().Add(time.Second)))
		_, _, err = second.ReadMessage()
	}
	require.Error(t, err, "duplicate handshake received an OPEN packet")
	require.NoError(t, server.Close())
	require.NoError(t, first.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err = first.ReadMessage()
	require.Error(t, err)
	if timeout, ok := err.(interface{ Timeout() bool }); ok {
		require.False(t, timeout.Timeout(), "first session was replaced and leaked")
	}
}
