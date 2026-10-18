package session

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

type heldCloseConn struct {
	scriptConn
	entered chan struct{}
	release chan struct{}
}

func (c *heldCloseConn) Close() error {
	close(c.entered)
	<-c.release
	return c.scriptConn.Close()
}

// Covers E20
func TestSessionReadyState(t *testing.T) {
	c := &heldCloseConn{entered: make(chan struct{}), release: make(chan struct{})}
	s := newTestSession(t, c, "polling")
	require.False(t, s.Upgraded())
	require.NoError(t, s.Init())
	require.Equal(t, StateOpen, s.ReadyState())
	// A recorded failure starts closing before Close is called.
	s.observe(reasonTransportError, io.ErrUnexpectedEOF)
	require.Equal(t, StateClosing, s.ReadyState())
	done := make(chan error, 1)
	go func() { done <- s.Close() }()
	<-c.entered
	require.Equal(t, StateClosing, s.ReadyState())
	close(c.release)
	require.NoError(t, <-done)
	require.Equal(t, StateClosed, s.ReadyState())
}

func TestManagerAllSnapshot(t *testing.T) {
	m := NewManager(nil)
	s := newTestSession(t, &scriptConn{}, "polling")
	m.Add(s)
	all := m.All()
	require.Same(t, s, all[s.ID()])
	delete(all, s.ID())
	require.Len(t, m.All(), 1)
	m.Remove(s.ID())
	require.Empty(t, m.All())
}
