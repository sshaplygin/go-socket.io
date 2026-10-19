package socketio

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"

	"github.com/sshaplygin/go-socket.io/parser"
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
func (c *rootOrderlyTransport) NextWriter(eioparser.FrameType, eioparser.PacketType) (io.WriteCloser, error) {
	return discardFrame{}, nil
}

type discardFrame struct{}

func (discardFrame) Write(b []byte) (int, error) { return len(b), nil }
func (discardFrame) Close() error                { return nil }

func rootWithOrderlyEngine(t *testing.T) (*connection, engineio.Conn, *rootOrderlyTransport) {
	t.Helper()
	tr := &rootOrderlyTransport{entered: make(chan struct{}), closed: make(chan struct{})}
	engine := engineio.NewServer(engineio.WithTransports([]transport.Transport{rootOrderlyFactory{tr}}...), engineio.WithPingInterval(time.Hour), engineio.WithPingTimeout(time.Hour))
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.test/?EIO=3&transport=polling", nil))
	s, err := engine.Accept()
	require.NoError(t, err)
	c := newConnection(s, newNamespaceRegistry(), 1, 50*time.Millisecond, slog.Default())
	c.boundary, c.connecting = true, false
	t.Cleanup(func() { c.stop(); _ = engineio.Abort(s); _ = engine.Close() })
	return c, s, tr
}

type rootOrderlyFactory struct{ conn *rootOrderlyTransport }

func (rootOrderlyFactory) Name() string { return "polling" }
func (f rootOrderlyFactory) Accept(http.ResponseWriter, *http.Request) (transport.Conn, error) {
	return f.conn, nil
}
func (rootOrderlyFactory) Dial(*url.URL, http.Header) (transport.Conn, error) {
	return nil, io.ErrClosedPipe
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
