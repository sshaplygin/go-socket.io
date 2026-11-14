package engineio

import (
	"net/http/httptest"
	"testing"

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

// A closed session leaves both the registry and the shutdown snapshot, so
// Server.Close has nothing left to close.
func TestClosedSessionLeavesRegistryAndShutdownSnapshot(t *testing.T) {
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
	s.lifecycle.Lock()
	live := len(s.live)
	s.lifecycle.Unlock()
	require.Zero(t, live)
	require.Zero(t, s.Count())
	require.NoError(t, s.Close())
	require.Zero(t, s.Count())
}
