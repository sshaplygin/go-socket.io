package socketio

import (
	"github.com/stretchr/testify/require"
	"sync/atomic"
	"testing"
)

// Covers G4
func TestRegressionG4CloseDoesNotDisconnectDuringConnect(t *testing.T) {
	entered := make(chan Conn, 1)
	release := make(chan struct{})
	var inConnect atomic.Bool
	disconnected := make(chan bool, 1)
	_, url := defectServer(t, func(s *Server) {
		s.OnConnect("/chat", func(c Conn) error { inConnect.Store(true); entered <- c; <-release; inConnect.Store(false); return nil })
		s.OnDisconnect("/chat", func(Conn, string) { disconnected <- inConnect.Load() })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send("0/chat,")
	nc := recv(t, entered, "OnConnect entered")
	done := make(chan struct{})
	go func() { _ = nc.Close(); close(done) }()
	recv(t, done, "Close returned without waiting on admission")
	close(release)
	require.False(t, recv(t, disconnected, "deferred OnDisconnect"), "OnDisconnect ran before OnConnect returned")
}
