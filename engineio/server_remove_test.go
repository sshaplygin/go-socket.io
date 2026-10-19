package engineio

import (
	"net/http/httptest"
	"testing"
	"time"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/stretchr/testify/require"
)

func TestRemoveMustNotEvadeShutdown(t *testing.T) {
	s := NewServer()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	d := engineclient.Dialer{Transports: []transport.Transport{websocket.Default}}
	peer, err := d.Dial(ts.URL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	conn, err := s.Accept()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	s.Remove(conn.ID())
	require.Zero(t, s.Count())
	require.NoError(t, s.Close())
	require.Equal(t, StateClosed, conn.ReadyState(), "Server.Close promises to close every accepted session, including unregistered sessions")
}

func TestCloseClearsClosedRegisteredSession(t *testing.T) {
	s := NewServer()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	d := engineclient.Dialer{Transports: []transport.Transport{websocket.Default}}
	peer, err := d.Dial(ts.URL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	conn, err := s.Accept()
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool { s.lifecycle.Lock(); defer s.lifecycle.Unlock(); return len(s.live) == 0 }, time.Second, time.Millisecond)
	require.Equal(t, 1, s.Count())
	require.NoError(t, s.Close())
	require.Zero(t, s.Count())
}
