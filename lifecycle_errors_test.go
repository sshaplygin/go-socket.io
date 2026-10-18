package socketio

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

// shortPingOptions returns engine.io options with a short pingTimeout, so a raw
// engine.io client (which never sends CLOSE) does not hold teardown. The last session
// of such a test closes by ping timeout around or after the end of the test, so its
// records go to a discarding logger: with the default one they would reach the
// slog.Default capture of whichever test runs next.
func shortPingOptions() []engineio.Option {
	return []engineio.Option{engineio.WithPingTimeout(500 * time.Millisecond), engineio.WithPingInterval(200 * time.Millisecond), engineio.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))}
}

// newTestServer starts srv behind httptest with shortPingOptions.
func newTestServer(t *testing.T, setup func(*Server)) *httptest.Server {
	t.Helper()
	srv := NewServer(WithEngineOptions(shortPingOptions()...))
	setup(srv)
	go func() { _ = srv.Serve() }()
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		_ = srv.Close()
		ts.Close()
	})
	return ts
}

// readClosed reports whether the server closed the connection: the next
// read fails instead of returning a packet.
func (c *rawClient) readClosed() bool {
	return strings.HasPrefix(c.read(), "error: ")
}

func TestLifecycleRootConnectError(t *testing.T) {
	rootErrs := make(chan error, 1)
	ts := newTestServer(t, func(srv *Server) {
		srv.OnConnect("/", func(Conn) error { return errors.New("rejected") })
		srv.OnError("/", func(c Conn, err error) {
			if c == nil {
				rootErrs <- err
			}
		})
	})

	c := dialRaw(t, ts.URL)
	require.EqualError(t, recv(t, rootErrs, "root OnError"), "rejected")
	require.Equal(t, "4\"rejected\"\n", c.read(), "root rejection answers ERROR")
	c.send("0/chat,")
}

func TestLifecycleNoRootHandler(t *testing.T) {
	ts := newTestServer(t, func(srv *Server) {
		srv.OnConnect("/chat", func(Conn) error { return nil })
	})

	c := dialRaw(t, ts.URL)
	require.True(t, c.readClosed(), "without a root handler the connection is closed")
}

func TestLifecycleHandlerPanicReachesOnError(t *testing.T) {
	errs := make(chan error, 1)
	ts := newTestServer(t, func(srv *Server) {
		srv.OnConnect("/", func(Conn) error { return nil })
		srv.OnEvent("/", "boom", func(Conn) { panic("handler failed") })
		srv.OnError("/", func(_ Conn, err error) { errs <- err })
	})

	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send(`2["boom"]`)
	require.ErrorContains(t, recv(t, errs, "OnError"), "handler failed")
	require.True(t, c.readClosed(), "a failing handler ends the connection")
}

func TestLifecycleEventDecodeError(t *testing.T) {
	errs := make(chan error, 1)
	ts := newTestServer(t, func(srv *Server) {
		srv.OnConnect("/", func(Conn) error { return nil })
		srv.OnEvent("/", "num", func(_ Conn, n int) int { return n })
		srv.OnError("/", func(_ Conn, err error) { errs <- err })
	})

	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send(`2["num","not a number"]`)
	require.Error(t, recv(t, errs, "OnError for a decode error"))
	c.send(`27["num",42]`)
	require.Equal(t, "37[42]\n", c.read(), "a decode error must not close the connection")
}

// TestLifecycleAcks covers both ack directions over a raw connection: a
// server Emit with a callback answered by the client, and client ACK packets
// for an unknown id or an unknown namespace, which the server ignores.
func TestLifecycleAcks(t *testing.T) {
	connected := make(chan Conn, 1)
	answers := make(chan string, 1)
	ts := newTestServer(t, func(srv *Server) {
		srv.OnConnect("/", func(s Conn) error {
			connected <- s
			return nil
		})
		srv.OnEvent("/", "ping", func(Conn) string { return "pong" })
	})

	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	sc := recv(t, connected, "OnConnect")

	sc.SetContext("state")
	require.Equal(t, "state", sc.Context())
	sc.Join("r")
	require.Contains(t, sc.Rooms(), "r")
	sc.Leave("r")
	require.NotContains(t, sc.Rooms(), "r")
	require.NotNil(t, sc.RemoteAddr())
	require.NotNil(t, sc.RemoteHeader())

	sc.Emit("ask", "q", func(answer string) { answers <- answer })
	require.Equal(t, `21["ask","q"]`+"\n", c.read(), "event with ack id 1")
	c.send(`31["a"]`)
	require.Equal(t, "a", recv(t, answers, "ack callback"))

	// Unknown ack id and unknown namespace are ignored; the connection stays usable.
	c.send(`399["late"]`)
	c.send(`3/nope,1[]`)
	c.send(`25["ping"]`)
	require.Equal(t, `35["pong"]`+"\n", c.read())
}

func TestLifecycleDisconnectHandlers(t *testing.T) {
	reasons := make(chan string, 2)
	ts := newTestServer(t, func(srv *Server) {
		srv.OnConnect("/", func(Conn) error { return nil })
		srv.OnConnect("/chat", func(Conn) error { return nil })
		srv.OnDisconnect("/chat", func(_ Conn, reason string) { reasons <- reason })
	})

	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send("0/chat")
	require.Equal(t, "0/chat,", c.read())

	// DISCONNECT for a namespace that was never connected is ignored.
	c.send("1/other")
	// A namespace DISCONNECT reports the client reason and keeps the session.
	c.send("1/chat")
	require.Equal(t, "client namespace disconnect", recv(t, reasons, "OnDisconnect(/chat)"))
	c.send("0/chat")
	require.Equal(t, "0/chat,", c.read(), "the connection is still usable")
}

// TestServerRegistrationOrder registers OnError, OnDisconnect and OnEvent
// before OnConnect, each creating the namespace, and removes a session.
func TestServerRegistrationOrder(t *testing.T) {
	srv := NewServer()
	srv.OnError("/a", func(Conn, error) {})
	srv.OnDisconnect("/b", func(Conn, string) {})
	srv.OnEvent("/c", "e", func(Conn) {})
	for _, nsp := range []string{"/a", "/b", "/c"} {
		require.NotNil(t, srv.getNamespace(nsp), nsp)
		require.Equal(t, 0, srv.RoomLen(nsp, "unjoined"), nsp)
	}
	srv.Remove("no-such-sid")
	require.Equal(t, 0, srv.Count())
}
