package socketio

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// heartbeatPair is a server with the given heartbeat and a Go client over tr. Both
// roles report OnError to errs and their OnDisconnect reasons to their channels.
type heartbeatPair struct {
	srv                         *Server
	cl                          *client.Client
	conns                       chan Conn
	errs                        chan error
	serverReasons, clientReason chan string
}

// newHeartbeatPair registers handlers through setup before the client connects.
func newHeartbeatPair(t *testing.T, tr transport.Transport, interval, timeout time.Duration, setup func(*heartbeatPair)) *heartbeatPair {
	t.Helper()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := &heartbeatPair{
		srv: NewServer(WithEngineOptions(engineio.WithPingInterval(interval), engineio.WithPingTimeout(timeout), engineio.WithLogger(quiet))),
		// Sized for every callback a test can cause, so no callback blocks on its report.
		conns: make(chan Conn, 1), errs: make(chan error, 16),
		serverReasons: make(chan string, 1), clientReason: make(chan string, 1),
	}
	p.srv.OnConnect("/", func(c Conn) error { p.conns <- c; return nil })
	p.srv.OnError("/", func(_ Conn, err error) { p.errs <- err })
	p.srv.OnDisconnect("/", func(_ Conn, reason string) { p.serverReasons <- reason })
	go func() { _ = p.srv.Serve() }()
	ts := httptest.NewServer(p.srv)
	cl, err := client.New(ts.URL, client.WithTransports(tr), client.WithReconnection(false), client.WithLogger(quiet))
	require.NoError(t, err)
	p.cl = cl
	t.Cleanup(func() { _ = cl.Close(); _ = p.srv.Close(); ts.Close() })
	cl.OnError(func(_ client.Conn, err error) { p.errs <- err })
	cl.OnDisconnect(func(_ client.Conn, reason string) { p.clientReason <- reason })
	setup(p)
	require.NoError(t, cl.Connect())
	return p
}

// A server handler that blocks past the heartbeat window ends its own connection: the
// connection's reader is the one that answers the client's PING, so no PONG is sent and
// the client disconnects with ping timeout. The handler's ACK is lost, and neither
// role's OnError is called.
func TestBlockingServerHandlerEndsItsConnection(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			p := newHeartbeatPair(t, tr, 100*time.Millisecond, 300*time.Millisecond, func(p *heartbeatPair) {
				p.srv.OnEvent("/", "work", func(Conn) string {
					close(started)
					<-release
					return "done"
				})
			})
			c := recv(t, p.conns, "OnConnect")
			acks := make(chan string, 1)
			p.cl.Emit("work", func(s string) { acks <- s })
			recv(t, started, "the handler to start")
			// The handler is released only once the client has given up on the server.
			require.Equal(t, "ping timeout", recv(t, p.clientReason, "the client's OnDisconnect"))
			close(release)
			recv(t, p.serverReasons, "the server's OnDisconnect")
			recv(t, serverCompletion(p.srv, c.ID()), "the end of the connection")
			require.Empty(t, drain(acks), "the ACK of the blocking handler arrived")
			require.Empty(t, drain(p.errs), "OnError was called")
		})
	}
}

// A Go client callback that blocks past the heartbeat window ends its own connection
// with ping timeout as well: the client reads no packet while it runs, so its heartbeat
// window expires, and over polling the server also stops hearing from it.
func TestBlockingClientHandlerEndsItsConnection(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			var running atomic.Bool
			started, release, pings := make(chan struct{}), make(chan struct{}), make(chan struct{}, 16)
			const interval, timeout = 100 * time.Millisecond, 300 * time.Millisecond
			p := newHeartbeatPair(t, tr, interval, timeout, func(p *heartbeatPair) {
				p.cl.OnPing(func() {
					if running.Load() {
						select {
						case pings <- struct{}{}:
						default:
						}
					}
				})
				p.cl.OnEvent("work", func(client.Conn) {
					running.Store(true)
					close(started)
					<-release
				})
			})
			c := recv(t, p.conns, "OnConnect")
			c.Emit("work")
			recv(t, started, "the client handler to start")
			// PINGs are at least interval apart, so after six of them the client has read
			// nothing for longer than interval+timeout. Over polling the server ends the
			// session first, after which the client sends no more PINGs.
			serverEnded := false
			for n := 0; n < 6 && !serverEnded; n++ {
				select {
				case <-pings:
				case reason := <-p.serverReasons:
					p.serverReasons <- reason
					serverEnded = true
				case <-time.After(waitFor):
					t.Fatal("timed out waiting for the client's PINGs")
				}
			}
			close(release)
			require.Equal(t, "ping timeout", recv(t, p.clientReason, "the client's OnDisconnect"))
			recv(t, p.serverReasons, "the server's OnDisconnect")
			require.Empty(t, drain(p.errs), "OnError was called")
		})
	}
}

// A handler that hands its work to a goroutine and acknowledges through AckFunc returns
// at once, so the heartbeat keeps the connection alive while the work runs and the ACK
// arrives. An AckFunc called after its connection ended does nothing.
func TestDeferredAckKeepsHeartbeat(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			var pending atomic.Bool
			pongs, late := make(chan struct{}, 16), make(chan AckFunc, 1)
			// A timeout that a loaded host keeps: only the heartbeat round trips are counted.
			p := newHeartbeatPair(t, tr, 50*time.Millisecond, 5*time.Second, func(p *heartbeatPair) {
				p.cl.OnPong(func(time.Duration) {
					if pending.Load() {
						select {
						case pongs <- struct{}{}:
						default:
						}
					}
				})
				p.srv.OnEvent("/", "work", func(_ Conn, ack AckFunc) {
					pending.Store(true)
					go func() {
						for i := 0; i < 3; i++ { // three heartbeat round trips while the work runs
							<-pongs
						}
						ack("done")
					}()
				})
				p.srv.OnEvent("/", "later", func(_ Conn, ack AckFunc) { late <- ack })
			})
			c := recv(t, p.conns, "OnConnect")
			acks := make(chan string, 1)
			p.cl.Emit("work", func(s string) { acks <- s })
			require.Equal(t, "done", recv(t, acks, "the deferred ACK"))
			require.Empty(t, drain(p.clientReason), "the client disconnected")
			require.Empty(t, drain(p.serverReasons), "the server disconnected")

			p.cl.Emit("later", func(string) { acks <- "late" })
			ack := recv(t, late, "the later handler")
			require.NoError(t, p.cl.Close())
			recv(t, p.serverReasons, "the server's OnDisconnect")
			recv(t, serverCompletion(p.srv, c.ID()), "the end of the connection")
			done := make(chan struct{})
			go func() { defer close(done); ack("late") }()
			recv(t, done, "the late AckFunc call")
			require.Empty(t, drain(acks), "an ACK arrived after the client closed")
			require.Empty(t, drain(p.errs), "OnError was called")
		})
	}
}
