package client

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/stretchr/testify/require"
)

type instantCloseClientConn struct {
	*fakeConn
	readEntered chan struct{}
	once        sync.Once
}

func (c *instantCloseClientConn) NextReader() (session.FrameType, io.ReadCloser, error) {
	c.once.Do(func() { close(c.readEntered) })
	return session.TEXT, nil, io.EOF
}

// Covers C3
func TestClientWorkersWaitForConnectAttempt(t *testing.T) {
	cl, err := New("http://unused/", &Options{ReconnectionDelay: time.Nanosecond, RandomizationFactor: -1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	fc := &instantCloseClientConn{fakeConn: newFakeConn(t), readEntered: make(chan struct{})}
	dialEntered, dialRelease := make(chan struct{}), make(chan struct{})
	var dialOnce sync.Once
	defer dialOnce.Do(func() { close(dialRelease) })
	cl.dial = func(string) (engineio.Conn, error) { close(dialEntered); <-dialRelease; return fc, nil }
	attempted := make(chan struct{}, 1)
	cl.OnReconnectAttempt(func(int) { _ = cl.Close(); attempted <- struct{}{} })
	result := make(chan error, 1)
	go func() { result <- cl.Connect() }()
	recv(t, dialEntered, "pending dial")
	cl.state.mu.Lock()
	attempt := cl.state.attempt
	cl.state.mu.Unlock()
	// Hold cancellation completion, making the window between OPEN and the
	// connect attempt's completion observable without scheduler luck.
	cancelHeld, cancelRelease := make(chan struct{}), make(chan struct{})
	var cancelOnce sync.Once
	defer cancelOnce.Do(func() { close(cancelRelease) })
	go attempt.once.Do(func() { close(cancelHeld); <-cancelRelease; close(attempt.stop) })
	recv(t, cancelHeld, "attempt completion held")
	dialOnce.Do(func() { close(dialRelease) })
	select {
	case <-fc.readEntered:
		t.Fatal("connection workers started before the connect attempt finished")
	case <-time.After(30 * time.Millisecond):
	}
	cancelOnce.Do(func() { close(cancelRelease) })
	require.NoError(t, recv(t, result, "Connect result"))
	recv(t, fc.readEntered, "immediate EOF after OPEN")
	recv(t, attempted, "immediate close schedules reconnect")
}
