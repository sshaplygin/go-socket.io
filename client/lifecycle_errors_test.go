package client_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/stretchr/testify/require"
)

// shortPingOptions returns engine.io options with a short pingTimeout, so a raw
// engine.io client (which never sends CLOSE) does not hold teardown. The last session
// of such a test closes by ping timeout around or after the end of the test, so its
// records go to a discarding logger: with the default one they would reach the
// slog.Default capture of whichever test runs next.
func shortPingOptions() *engineio.Options {
	return &engineio.Options{
		PingTimeout:  500 * time.Millisecond,
		PingInterval: 200 * time.Millisecond,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// newTestServer starts srv behind httptest with shortPingOptions.
func newTestServer(t *testing.T, setup func(*socketio.Server)) *httptest.Server {
	t.Helper()
	srv := socketio.NewServer(shortPingOptions())
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
	ts := newTestServer(t, func(srv *socketio.Server) {
		srv.OnConnect("/", func(client.Conn) error { return errors.New("rejected") })
		srv.OnError("/", func(c client.Conn, err error) {
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
	ts := newTestServer(t, func(srv *socketio.Server) {
		srv.OnConnect("/chat", func(client.Conn) error { return nil })
	})

	c := dialRaw(t, ts.URL)
	require.True(t, c.readClosed(), "without a root handler the connection is closed")
}

func TestLifecycleHandlerPanicReachesOnError(t *testing.T) {
	errs := make(chan error, 1)
	ts := newTestServer(t, func(srv *socketio.Server) {
		srv.OnConnect("/", func(client.Conn) error { return nil })
		srv.OnEvent("/", "boom", func(client.Conn) { panic("handler failed") })
		srv.OnError("/", func(_ client.Conn, err error) { errs <- err })
	})

	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send(`2["boom"]`)
	require.ErrorContains(t, recv(t, errs, "OnError"), "handler failed")
	require.True(t, c.readClosed(), "a failing handler ends the connection")
}

func TestLifecycleEventDecodeError(t *testing.T) {
	errs := make(chan error, 1)
	ts := newTestServer(t, func(srv *socketio.Server) {
		srv.OnConnect("/", func(client.Conn) error { return nil })
		srv.OnEvent("/", "num", func(_ client.Conn, n int) int { return n })
		srv.OnError("/", func(_ client.Conn, err error) { errs <- err })
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
	connected := make(chan client.Conn, 1)
	answers := make(chan string, 1)
	ts := newTestServer(t, func(srv *socketio.Server) {
		srv.OnConnect("/", func(s client.Conn) error {
			connected <- s
			return nil
		})
		srv.OnEvent("/", "ping", func(client.Conn) string { return "pong" })
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
	ts := newTestServer(t, func(srv *socketio.Server) {
		srv.OnConnect("/", func(client.Conn) error { return nil })
		srv.OnConnect("/chat", func(client.Conn) error { return nil })
		srv.OnDisconnect("/chat", func(_ client.Conn, reason string) { reasons <- reason })
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

// rawServer is an engine.io server whose single session the test drives
// packet by packet, to exercise the Go client's packet handlers.
type rawServer struct {
	t    *testing.T
	conn chan engineio.Conn
	url  string
}

func newRawServer(t *testing.T) *rawServer {
	t.Helper()
	eio := engineio.NewServer(shortPingOptions())
	ts := httptest.NewServer(eio)
	rs := &rawServer{t: t, conn: make(chan engineio.Conn, 1), url: ts.URL}
	go func() {
		c, err := eio.Accept()
		if err == nil {
			rs.conn <- c
		}
	}()
	t.Cleanup(func() {
		_ = eio.Close()
		ts.Close()
	})
	return rs
}

func (rs *rawServer) accept() *rawClient {
	rs.t.Helper()
	c := recv(rs.t, rs.conn, "engine.io session")
	rs.t.Cleanup(func() { _ = c.Close() })
	return &rawClient{t: rs.t, conn: c}
}

// connectRawClient connects a Go client with the given handlers to a raw
// engine.io server and completes the root CONNECT exchange.
func connectRawClient(t *testing.T, setup func(*client.Client)) *rawClient {
	t.Helper()
	rs := newRawServer(t)
	cl, err := client.NewClient(rs.url, nil)
	require.NoError(t, err)
	connected := make(chan struct{}, 1)
	cl.OnConnect(func(client.Conn) error {
		connected <- struct{}{}
		return nil
	})
	setup(cl)
	require.NoError(t, cl.Connect())
	t.Cleanup(func() { _ = cl.Close() })

	srv := rs.accept()
	require.Equal(t, "0", srv.read(), "the client sends CONNECT for the root namespace")
	srv.send("0")
	recv(t, connected, "client OnConnect")
	return srv
}

// TestClientEventAndDisconnect drives the Go client through server CONNECT,
// an event and a server DISCONNECT.
func TestClientEventAndDisconnect(t *testing.T) {
	events := make(chan int, 1)
	reasons := make(chan string, 1)
	srv := connectRawClient(t, func(cl *client.Client) {
		cl.OnEvent("num", func(_ client.Conn, n int) { events <- n })
		cl.OnDisconnect(func(_ client.Conn, reason string) { reasons <- reason })
	})

	srv.send(`2["num",5]`)
	require.Equal(t, 5, recv(t, events, "client event"))

	// Known defect, pinned until stage 2.3 defines disconnect reasons: a server
	// DISCONNECT reaches the client's OnDisconnect with an empty reason.
	srv.send("1")
	require.Equal(t, "", recv(t, reasons, "client OnDisconnect"))
}

// TestClientDecodeErrorReachesOnError sends a malformed event to the Go
// client; the decode error is reported to OnError.
func TestClientDecodeErrorReachesOnError(t *testing.T) {
	errs := make(chan error, 1)
	srv := connectRawClient(t, func(cl *client.Client) {
		cl.OnEvent("num", func(client.Conn, int) {})
		cl.OnError(func(_ client.Conn, err error) { errs <- err })
	})

	srv.send(`2["num","five"]`)
	require.Error(t, recv(t, errs, "client OnError"))
}

func TestClientRejectsEmptyAddr(t *testing.T) {
	_, err := client.NewClient("", nil)
	require.ErrorIs(t, err, client.ErrEmptyAddr)
	_, err = client.NewClient("http://[::1", nil)
	require.Error(t, err)
}

// TestServerRegistrationOrder registers OnError, OnDisconnect and OnEvent
// before OnConnect, each creating the namespace, and removes a session.
func TestServerRegistrationOrder(t *testing.T) {
	srv := socketio.NewServer(nil)
	srv.OnError("/a", func(client.Conn, error) {})
	srv.OnDisconnect("/b", func(client.Conn, string) {})
	srv.OnEvent("/c", "e", func(client.Conn) {})
	for _, nsp := range []string{"/a", "/b", "/c"} {
		require.Equal(t, 0, srv.RoomLen(nsp, "unjoined"), nsp)
	}
	srv.Remove("no-such-sid")
	require.Equal(t, 0, srv.Count())
}
