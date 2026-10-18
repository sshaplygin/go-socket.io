package engineio

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// Covers E17
// Covers E16
func TestEngineClients(t *testing.T) {
	s := NewServer()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	d := engineclient.Dialer{Transports: []transport.Transport{websocket.Default}}
	a, err := d.Dial(ts.URL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = a.Close() })
	first, err := s.Accept()
	require.NoError(t, err)
	b, err := d.Dial(ts.URL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })
	second, err := s.Accept()
	require.NoError(t, err)
	require.Len(t, s.Clients(), 2)
	copy := s.Clients()
	delete(copy, first.ID())
	require.Len(t, s.Clients(), 2)
	require.Same(t, second, s.Clients()[second.ID()])
	require.NoError(t, first.Close())
	require.Equal(t, StateClosed, first.ReadyState())
	require.Len(t, s.Clients(), 1)
	require.Equal(t, 2, s.Count(), "closed sessions remain registered until Remove")
	s.Remove(first.ID())
	require.Equal(t, 1, s.Count())
	require.NoError(t, s.Close())
	require.Empty(t, s.Clients())
}

// Covers E20
func TestEngineConnState(t *testing.T) {
	t.Run("upgrade", func(t *testing.T) { testEngineUpgrade(t, false) })
	t.Run("close", func(t *testing.T) {
		s := NewServer()
		c := &stateCloseConn{blockingConn: *newBlockingConn(), entered: make(chan struct{}), release: make(chan struct{})}
		var release sync.Once
		defer release.Do(func() { close(c.release) })
		t.Cleanup(func() { _ = s.Close() })
		_, err := s.newSession(httptest.NewRequest(http.MethodGet, "/", nil), c, "polling")
		require.NoError(t, err)
		conn, err := s.Accept()
		require.NoError(t, err)
		require.Equal(t, StateOpen, conn.ReadyState())
		done := make(chan error, 1)
		go func() { done <- conn.Close() }()
		select {
		case <-c.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("transport Close did not start")
		}
		require.Equal(t, StateClosing, conn.ReadyState())
		require.Same(t, conn, s.Clients()[conn.ID()], "closing sessions remain visible")
		release.Do(func() { close(c.release) })
		require.NoError(t, <-done)
		require.Equal(t, StateClosed, conn.ReadyState())
		require.Empty(t, s.Clients())
	})
}

type stateCloseConn struct {
	blockingConn
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (c *stateCloseConn) Close() error {
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return c.blockingConn.Close()
}

func (c *stateCloseConn) NextWriter(eioparser.FrameType, eioparser.PacketType) (io.WriteCloser, error) {
	return stateWriter{io.Discard}, nil
}

type stateWriter struct{ io.Writer }

func (stateWriter) Close() error { return nil }
