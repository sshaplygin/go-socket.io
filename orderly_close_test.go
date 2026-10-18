package socketio

import (
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/parser"
	"github.com/stretchr/testify/require"
)

// This transport can only complete its orderly close when forced to abort.
// Using a real Session verifies root's private abort bridge, rather than a fake
// engine whose Close cannot distinguish the two operations.
type rootOrderlyTransport struct {
	transport.Conn
	entered, closed chan struct{}
	once            sync.Once
}

func (c *rootOrderlyTransport) CloseGracefully() error {
	close(c.entered)
	<-c.closed
	return io.EOF
}
func (c *rootOrderlyTransport) Close() error                     { c.once.Do(func() { close(c.closed) }); return nil }
func (c *rootOrderlyTransport) SetReadDeadline(time.Time) error  { return nil }
func (c *rootOrderlyTransport) SetWriteDeadline(time.Time) error { return nil }
func (c *rootOrderlyTransport) RemoteAddr() net.Addr             { return nil }
func (c *rootOrderlyTransport) NextWriter(frame.Type, packet.Type) (io.WriteCloser, error) {
	return discardFrame{}, nil
}

type discardFrame struct{}

func (discardFrame) Write(b []byte) (int, error) { return len(b), nil }
func (discardFrame) Close() error                { return nil }

func rootWithOrderlyEngine(t *testing.T) (*conn, *session.Session, *rootOrderlyTransport) {
	t.Helper()
	tr := &rootOrderlyTransport{entered: make(chan struct{}), closed: make(chan struct{})}
	s, err := session.New(tr, "orderly", "polling", transport.ConnParameters{PingInterval: time.Hour, PingTimeout: time.Hour}, nil)
	require.NoError(t, err)
	require.NoError(t, s.InitSession())
	c := newConn(s, newNamespaceHandlers(), connLimits{writeBufferSize: 1, drainTimeout: 50 * time.Millisecond}, slog.Default())
	c.boundary, c.connecting = true, false
	t.Cleanup(func() { c.stop(); _ = s.Abort() })
	return c, s, tr
}

func TestRootDrainDeadlineAbortsOrderlyEngine(t *testing.T) {
	c, s, tr := rootWithOrderlyEngine(t)
	require.NoError(t, c.Close())
	go c.serveWrite()
	recv(t, tr.entered, "engine orderly close after queue drain")
	require.Equal(t, engineio.StateClosing, s.ReadyState())
	recv(t, tr.closed, "root deadline interrupts engine orderly wait")
	require.Eventually(t, func() bool { return s.ReadyState() == engineio.StateClosed }, time.Second, time.Millisecond)
}

func TestRootOverflowAbortsOrderlyEngine(t *testing.T) {
	c, s, tr := rootWithOrderlyEngine(t)
	require.NoError(t, s.Close())
	recv(t, tr.entered, "engine orderly close")
	// The root has not started its own drain; overflow must still interrupt
	// an orderly engine close initiated directly by the application.
	c.write(parser.Header{Type: parser.Event})
	c.write(parser.Header{Type: parser.Event})
	recv(t, tr.closed, "overflow abort")
	require.Eventually(t, func() bool { return s.ReadyState() == engineio.StateClosed }, time.Second, time.Millisecond)
}
