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
// role's OnError is called. The test pins the behavior of a reader that also answers
// the heartbeat, which the documentation does not promise: a server that answered PING
// while a handler runs would keep this connection.
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

// blockedClient observes the heartbeat of a Go client while one of its callbacks blocks.
type blockedClient struct {
	running atomic.Bool
	pongs   atomic.Int32
	pings   chan struct{}
}

// observe counts the client's PINGs and the PONGs it reads once block has been called.
func (b *blockedClient) observe(cl *client.Client) {
	b.pings = make(chan struct{}, 16)
	cl.OnPing(func() {
		if b.running.Load() {
			select {
			case b.pings <- struct{}{}:
			default:
			}
		}
	})
	cl.OnPong(func(time.Duration) {
		if b.running.Load() {
			b.pongs.Add(1)
		}
	})
}

// block is called by the callback before it blocks.
func (b *blockedClient) block() { b.running.Store(true) }

// pastWindow returns once the blocked callback has outlasted the heartbeat window. PINGs
// are at least pingInterval apart, so after six of them, with the 100 ms interval and
// 300 ms timeout of these tests, the client has read nothing for longer than the window.
// Over polling the server can end the session first, after which the client sends no
// more PINGs.
func (b *blockedClient) pastWindow(t *testing.T, p *heartbeatPair) {
	t.Helper()
	for n := 0; n < 6; n++ {
		select {
		case <-b.pings:
		case reason := <-p.serverReasons:
			p.serverReasons <- reason
			return
		case <-time.After(waitFor):
			t.Fatal("timed out waiting for the client's PINGs")
		}
	}
}

// While a Go client callback runs, the client's heartbeat stops: its PINGs go out, but it
// reads no PONG, because the reader that would read one is running the callback. The
// client renews its window only when it reads a PONG, so a callback that blocks past the
// window normally ends its connection with ping timeout. The end is not guaranteed: over
// websocket, a PONG read ahead together with the event renews the window once the
// callback returns. So after the release the test accepts either outcome; it requires
// ping timeout if the connection ends, and no OnError in both cases.
func TestBlockingClientHandlerStopsHeartbeat(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			var blocked blockedClient
			started, release := make(chan struct{}), make(chan struct{})
			const interval, timeout = 100 * time.Millisecond, 300 * time.Millisecond
			p := newHeartbeatPair(t, tr, interval, timeout, func(p *heartbeatPair) {
				blocked.observe(p.cl)
				p.cl.OnEvent("work", func(client.Conn) {
					blocked.block()
					close(started)
					<-release
				})
				p.cl.OnEvent("probe", func(client.Conn) string { return "alive" })
			})
			c := recv(t, p.conns, "OnConnect")
			c.Emit("work")
			recv(t, started, "the client handler to start")
			blocked.pastWindow(t, p)
			require.Zero(t, blocked.pongs.Load(), "the client read a PONG while its callback ran")
			close(release)
			alive := make(chan string, 1)
			c.Emit("probe", func(s string) { alive <- s })
			select {
			case reason := <-p.clientReason:
				require.Equal(t, "ping timeout", reason)
				recv(t, p.serverReasons, "the server's OnDisconnect")
			case <-alive:
				t.Log("the connection survived: a PONG read ahead renewed the client's window")
			case <-time.After(waitFor):
				t.Fatal("neither a disconnect nor the probe's ACK")
			}
			require.Empty(t, drain(p.errs), "OnError was called")
		})
	}
}

// A handler that hands its work to a goroutine and acknowledges through AckFunc returns
// at once, so the heartbeat keeps the connection alive while the work runs and the ACK
// arrives.
func TestDeferredAckKeepsHeartbeat(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			var pending atomic.Bool
			pongs := make(chan struct{}, 16)
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
			})
			recv(t, p.conns, "OnConnect")
			acks := make(chan string, 1)
			p.cl.Emit("work", func(s string) { acks <- s })
			require.Equal(t, "done", recv(t, acks, "the deferred ACK"))
			require.Empty(t, drain(p.clientReason), "the client disconnected")
			require.Empty(t, drain(p.serverReasons), "the server disconnected")
			require.Empty(t, drain(p.errs), "OnError was called")
		})
	}
}
