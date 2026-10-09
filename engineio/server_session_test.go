package engineio

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

// blockingConn is a transport.Conn whose NextReader and NextWriter block
// until Close, so a session's handshake stays in flight.
type blockingConn struct {
	once   sync.Once
	closed chan struct{}
}

func newBlockingConn() *blockingConn { return &blockingConn{closed: make(chan struct{})} }

func (c *blockingConn) NextReader() (frame.Type, packet.Type, io.ReadCloser, error) {
	<-c.closed
	return 0, 0, nil, io.EOF
}

func (c *blockingConn) NextWriter(frame.Type, packet.Type) (io.WriteCloser, error) {
	<-c.closed
	return nil, io.EOF
}

func (c *blockingConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *blockingConn) URL() url.URL                     { return url.URL{} }
func (c *blockingConn) LocalAddr() net.Addr              { return nil }
func (c *blockingConn) RemoteAddr() net.Addr             { return nil }
func (c *blockingConn) RemoteHeader() http.Header        { return nil }
func (c *blockingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *blockingConn) SetWriteDeadline(time.Time) error { return nil }

// TestNewSessionRegistersBeforeHandshake checks that a new session can be
// found by its sid as soon as newSession returns, before the OPEN packet has
// been written: the client may send its next request with that sid as soon
// as it reads OPEN. A session whose handshake fails is removed again.
func TestNewSessionRegistersBeforeHandshake(t *testing.T) {
	// The handshake goroutine removes the session and then logs the rejection: wait for that
	// record, or it reaches the slog.Default recorder of the next log test.
	rec := newRecorder()
	s := NewServer(&Options{Logger: slog.New(rec)})
	conn := newBlockingConn()

	ses, err := s.newSession(context.Background(), conn, "polling")
	require.NoError(t, err)

	_, ok := s.sessions.Get(ses.ID())
	require.True(t, ok, "session not registered while its handshake is in flight")

	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool {
		_, ok := s.sessions.Get(ses.ID())
		return !ok
	}, 5*time.Second, 10*time.Millisecond, "session with a failed handshake stayed registered")
	require.Eventually(t, func() bool { return len(rec.find("engineio: request rejected")) == 1 },
		5*time.Second, time.Millisecond, "failed handshake not logged")
}
