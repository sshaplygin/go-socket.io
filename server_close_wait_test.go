package socketio

import (
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

// Covers S17
func TestServerCloseWaits(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	s, url := defectServer(t, func(s *Server) {
		s.OnDisconnect("/", func(Conn, string) { close(entered); <-release })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	recv(t, entered, "OnDisconnect entered")
	select {
	case err := <-closed:
		t.Fatalf("Close returned before the handler: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	require.NoError(t, recv(t, closed, "Server.Close"))
	require.Zero(t, s.Count())
	require.Empty(t, s.Rooms("/"))
}
