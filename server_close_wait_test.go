package socketio

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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

func TestServerCloseWaitsForExternalDisconnect(t *testing.T) {
	for _, namespaceOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "connection", true: "namespace"}[namespaceOnly], func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			connected := make(chan Conn, 1)
			s, url := defectServer(t, func(s *Server) {
				s.OnConnect("/", func(c Conn) error { connected <- c; return nil })
				s.OnDisconnect("/", func(Conn, string) { close(entered); <-release })
			})
			wire := dialRaw(t, url)
			require.Equal(t, "0", wire.read())
			c := recv(t, connected, "connected socket")
			disconnected := make(chan struct{})
			go func() {
				defer close(disconnected)
				if namespaceOnly {
					c.Disconnect()
				} else {
					_ = c.Close()
				}
			}()
			recv(t, entered, "external OnDisconnect")
			closed := make(chan error, 1)
			go func() { closed <- s.Close() }()
			select {
			case err := <-closed:
				t.Fatalf("Server.Close returned before external callback: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			once.Do(func() { close(release) })
			recv(t, disconnected, "external disconnect completed")
			require.NoError(t, recv(t, closed, "Server.Close"))
		})
	}
}
