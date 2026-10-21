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
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("repeated Close waited for Error hook")
	}
	require.False(t, c.isClosed())
	unblock()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Close did not return")
	}

	require.True(t, c.isClosed())
}

func TestErrorHookCanCloseSession(t *testing.T) {
	for _, tc := range []struct {
		name  string
		close func(*Session) error
	}{
		{"close", (*Session).Close}, {"abort", (*Session).Abort}, {"shutdown", (*Session).Shutdown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &scriptConn{}
			s := newTestSession(t, c, "polling")
			require.NoError(t, s.Init())
			result := make(chan error, 1)
			s.SetHooks(&Hooks{Error: func(s *Session, _ error) { result <- tc.close(s) }})
			s.observe(reasonTransportError, errors.New("peer reset"))
			go func() { _ = s.Close() }()
			select {
			case err := <-result:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("Error hook deadlocked calling close")
			}
			waitClose(t, s.closeDone)
			require.True(t, c.isClosed())
			require.Equal(t, ReasonTransportError, s.closeError().Reason)
		})
	}
}
