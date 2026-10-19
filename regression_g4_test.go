package socketio

import (
	"testing"
	"time"
	"github.com/stretchr/testify/require"
)

// Covers: G4
func TestRegressionG4CloseDoesNotDisconnectDuringConnect(t *testing.T) {
	entered := make(chan Conn, 1)
	release := make(chan struct{})
	disconnected := make(chan struct{}, 1)
	_, url := defectServer(t, func(s *Server) {
		s.OnConnect("/chat", func(c Conn) error { entered <- c; <-release; return nil })
		s.OnDisconnect("/chat", func(Conn, string) { disconnected <- struct{}{} })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send("0/chat,")
	nc := recv(t, entered, "OnConnect entered")
	done := make(chan struct{})
	go func() { _ = nc.Close(); close(done) }()
	select {
	case <-disconnected:
		t.Error("OnDisconnect ran before OnConnect returned")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	recv(t, done, "Close finished")
}

