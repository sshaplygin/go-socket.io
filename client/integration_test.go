package client

import (
	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

// TestOptionsReachConnections checks the queue size and drain deadline each connection
// uses. The public constructor resolves defaults; a held real polling writer
// verifies the resulting queue budget. Exact stored values remain owner-local.
//
// Covers 1I-T2 (C).
// Covers 1I-T3 (C).
// Covers 1I-T4 (C).
// Covers 1I-T5 (C).
// Covers 1I-T6 (C).
func TestOptionsReachConnections(t *testing.T) {
	cases := []struct {
		name  string
		opts  []Option
		size  int
		drain time.Duration
	}{
		{"nil options", nil, 64, time.Minute},
		{"zero", []Option{}, 64, time.Minute},
		{"dial timeout only", []Option{WithTimeout(3 * time.Second)}, 64, time.Minute},
		{"negative", []Option{WithWriteBufferSize(-1), WithCloseTimeout(-time.Second)}, 64, time.Minute},
		{"custom", []Option{WithWriteBufferSize(5), WithCloseTimeout(3 * time.Second)}, 5, 3 * time.Second},
		{"ordered", []Option{WithWriteBufferSize(2), WithCloseTimeout(time.Second), WithWriteBufferSize(5), WithCloseTimeout(3 * time.Second)}, 5, 3 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := url.Parse("http://127.0.0.1/")
			require.NoError(t, err)
			_, _, config := prepareOptions(parsed, tc.opts...)
			require.Equal(t, tc.size, config.WriteBufferSize)
			require.Equal(t, tc.drain, config.CloseTimeout)
			server := socketio.NewServer(socketio.WithEngineOptions(engineio.WithPingInterval(time.Hour)))
			server.OnConnect("/", func(socketio.Conn) error { return nil })
			go func() { _ = server.Serve() }()
			var armed atomic.Bool
			held, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			httpServer := httptest.NewServer(server)
			t.Cleanup(func() { unblock(); _ = server.Close(); httpServer.Close() })
			heldTransport := holdingTransport{Transport: polling.Default, armed: &armed, held: held, release: release}
			opts := append(append([]Option(nil), tc.opts...), WithTransports(heldTransport))
			cl, err := New(httpServer.URL, opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			connected, errors := make(chan Conn, 1), make(chan error, 1)
			cl.OnConnect(func(c Conn) error { connected <- c; return nil })
			cl.OnError(func(_ Conn, err error) { errors <- err })
			require.NoError(t, cl.Connect())
			c := receive(t, connected, "CONNECT")
			armed.Store(true)
			c.Emit("hold")
			receive(t, held, "held polling writer")
			for i := 0; i < tc.size; i++ {
				c.Emit("queued", i)
			}
			require.Empty(t, errors, "resolved budget accepts every slot")
			c.Emit("overflow")
			require.ErrorIs(t, receive(t, errors, "queue overflow"), ErrWriteBufferFull)
		})
	}
}

func receive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout: %s", what)
		var zero T
		return zero
	}
}

// Public defaults remain exercised through the constructor and its owning resolver.
func TestClientBackoff(t *testing.T) {
	cl, err := New("http://unused")
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	u, err := url.Parse("http://unused")
	require.NoError(t, err)
	_, _, config := prepareOptions(u)
	require.Equal(t, time.Second, config.ReconnectionDelay)
	require.Equal(t, 5*time.Second, config.ReconnectionDelayMax)
	require.Equal(t, 0.5, config.RandomizationFactor)
}

// holdingTransport stalls a MESSAGE at the public transport boundary. The real
// Engine.IO and Socket.IO peers still perform handshake, framing and shutdown.
type holdingTransport struct {
	transport.Transport
	armed   *atomic.Bool
	held    chan struct{}
	release <-chan struct{}
}

func (h holdingTransport) Dial(u *url.URL, headers http.Header) (transport.Conn, error) {
	c, err := h.Transport.Dial(u, headers)
	if err != nil {
		return nil, err
	}
	return holdingTransportConn{Conn: c, gate: h}, nil
}

type holdingTransportConn struct {
	transport.Conn
	gate holdingTransport
}

func (h holdingTransportConn) NextWriter(ft eioparser.FrameType, pt eioparser.PacketType) (io.WriteCloser, error) {
	if pt == eioparser.MESSAGE && h.gate.armed.CompareAndSwap(true, false) {
		close(h.gate.held)
		<-h.gate.release
	}
	return h.Conn.NextWriter(ft, pt)
}

func (h holdingTransportConn) Open() (eioparser.ConnParameters, error) {
	return h.Conn.(engineclient.Opener).Open()
}
