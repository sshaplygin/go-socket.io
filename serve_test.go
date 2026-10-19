package socketio

import (
	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestServeReturnsNilAfterClose checks that Serve treats the end of the
// accept loop caused by Close as a normal shutdown.
func TestServeReturnsNilAfterClose(t *testing.T) {
	srv := NewServer()
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve() }()

	require.NoError(t, srv.Close())
	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after Close")
	}
}

func TestFailedRootRemovesEngineSession(t *testing.T) {
	s := NewServer(WithEngineOptions(shortPingOptions()...), WithWriteBufferSize(1))
	s.OnConnect("/", func(c Conn) error { c.Emit("one"); c.Emit("two"); return nil })
	go func() { _ = s.Serve() }()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	ws, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	defer func() { _ = ws.Close() }()
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(time.Second)))
	for err == nil {
		_, _, err = ws.ReadMessage()
	}
	require.Eventually(t, func() bool { return s.Engine().Count() == 0 }, 300*time.Millisecond, 5*time.Millisecond, "closed failed-root session remains in engine registry")
}

func TestSuccessfulSessionRemovedAfterPeerClose(t *testing.T) {
	s, url := defectServer(t, func(*Server) {})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	require.Equal(t, 1, s.Engine().Count())
	require.NoError(t, c.conn.Close())
	require.Eventually(t, func() bool { return s.Engine().Count() == 0 }, time.Second, time.Millisecond)
}
