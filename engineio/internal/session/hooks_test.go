package session

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionHooksObserveHandshakeFailure(t *testing.T) {
	want := errors.New("handshake writer failure")
	c := &scriptConn{writerErr: want}
	s := newTestSession(t, c, "polling")
	var causes []error
	var states []ReadyState
	s.SetHooks(&Hooks{Error: func(s *Session, err error) { causes = append(causes, err); states = append(states, s.ReadyState()) }})
	require.ErrorIs(t, s.Init(), want)
	require.Equal(t, []error{want}, causes)
	require.Equal(t, []ReadyState{StateClosing}, states)
	require.Equal(t, StateClosed, s.ReadyState())
	require.NoError(t, s.Close())
	require.Len(t, causes, 1)
}

func TestSessionConcurrentCloseKeepsErrorBeforeTransportClose(t *testing.T) {
	c := &scriptConn{}
	s := newTestSession(t, c, "polling")
	require.NoError(t, s.Init())
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s.SetHooks(&Hooks{Error: func(*Session, error) { close(entered); <-release }})
	s.observe(reasonTransportError, errors.New("peer failed"))
	done := make(chan error, 2)
	go func() { done <- s.Close() }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("Error hook did not start")
	}
	go func() { done <- s.Close() }()
	select {
	case err := <-done:
		t.Fatalf("transport closed before Error returned: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	require.False(t, c.isClosed())
	unblock()
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("Close did not return")
		}
	}
	require.True(t, c.isClosed())
}
