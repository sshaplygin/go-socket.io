package socketio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServerCloseBound(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	s, url := defectServer(t, func(s *Server) {
		s.OnDisconnect("/", func(Conn, string) { close(entered); <-release })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	defer close(release)
	closed := make(chan error, 1)
	go func() { closed <- s.closeWithTimeout(100 * time.Millisecond) }()
	recv(t, entered, "blocked OnDisconnect")
	require.NoError(t, recv(t, closed, "bounded Close"))
}
