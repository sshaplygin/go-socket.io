package socketio

import (
	"context"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// closeCallbackCase is a server callback that calls Server.Close.
type closeCallbackCase struct {
	name string
	// root marks callbacks that run before the handshake completes, so a closing
	// server may refuse the dial.
	root bool
	// configure registers the callback, which calls closeFn once.
	configure func(s *Server, closeFn func())
	// trigger makes the server run that callback.
	trigger func(c *rawClient)
}

// closeCallbackCases returns the callbacks a server invokes for a connection;
// each call returns fresh cases, so a case's configure and trigger may share state.
func closeCallbackCases() []closeCallbackCase {
	accept := func(Conn) error { return nil }
	event := func(c *rawClient) { c.send(`2["shutdown"]`) }
	disconnect := func(c *rawClient) { c.send("1") }
	return []closeCallbackCase{
		{name: "root OnConnect", root: true, configure: func(s *Server, f func()) {
			s.OnConnect("/", func(Conn) error { f(); return nil })
		}},
		{name: "root middleware", root: true, configure: func(s *Server, f func()) {
			s.Use("/", func(Conn) error { f(); return nil })
			s.OnConnect("/", accept)
		}},
		{name: "namespace OnConnect", configure: func(s *Server, f func()) {
			s.OnConnect("/", accept)
			s.Of("/chat").OnConnect(func(Conn) error { f(); return nil })
		}, trigger: func(c *rawClient) { c.send("0/chat") }},
		{name: "OnEvent", configure: func(s *Server, f func()) {
			s.OnConnect("/", accept)
			s.OnEvent("/", "shutdown", func(Conn) { f() })
		}, trigger: event},
		{name: "namespace listener", configure: func(s *Server, f func()) {
			s.OnConnect("/", accept)
			s.Of("/").On("shutdown", func(Conn) { f() })
		}, trigger: event},
		{name: "Conn middleware", configure: func(s *Server, f func()) {
			s.OnConnect("/", func(c Conn) error {
				c.Use(func(string, []interface{}) error { f(); return nil })
				return nil
			})
			s.OnEvent("/", "shutdown", func(Conn) {})
		}, trigger: event},
		{name: "OnError", configure: func(s *Server, f func()) {
			s.OnConnect("/", accept)
			s.OnEvent("/", "shutdown", func(Conn, int) {})
			s.OnError("/", func(Conn, error) { f() })
		}, trigger: func(c *rawClient) { c.send(`2["shutdown","not a number"]`) }},
		{name: "acknowledgement", configure: func(s *Server, f func()) {
			s.OnConnect("/", func(c Conn) error { c.Emit("ask", func() { f() }); return nil })
		}, trigger: func(c *rawClient) {
			require.Equal(c.t, "0", c.read())
			require.Equal(c.t, "21[\"ask\"]\n", c.read())
			c.send("31[]")
		}},
		{name: "OnDisconnect", configure: func(s *Server, f func()) {
			s.OnConnect("/", accept)
			s.OnDisconnect("/", func(Conn, string) { f() })
		}, trigger: disconnect},
		{name: "disconnecting", configure: func(s *Server, f func()) {
			s.OnConnect("/", accept)
			s.Of("/").On("disconnecting", func(Conn, string) { f() })
		}, trigger: disconnect},
		// Conn.Close runs OnDisconnect on the goroutine that calls it, here one
		// of the application.
		{name: "OnDisconnect from Conn.Close", configure: func(s *Server, f func()) {
			s.OnConnect("/", accept)
			s.OnDisconnect("/", func(Conn, string) { f() })
			s.OnEvent("/", "close", func(c Conn) { go func() { _ = c.Close() }() })
		}, trigger: func(c *rawClient) { c.send(`2["close"]`) }},
	}
}

// closeCallbackServer starts a server configured by tc whose callback runs closeFn,
// and makes it run that callback over tr. Serve's result is sent to the channel.
func closeCallbackServer(t *testing.T, s *Server, tc closeCallbackCase, tr transport.Transport, closeFn func()) <-chan error {
	t.Helper()
	var once sync.Once
	tc.configure(s, func() { once.Do(closeFn) })
	served := make(chan error, 1)
	go func() { served <- s.Serve() }()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	conn, err := (&engineclient.Dialer{Transports: []transport.Transport{tr}}).Dial(ts.URL, nil)
	if err != nil {
		// A server that closes in a root admission callback can end the session
		// before the handshake response reaches the client.
		require.True(t, tc.root, "dial: %v", err)
		return served
	}
	t.Cleanup(func() { _ = conn.Close() })
	if tc.trigger != nil {
		tc.trigger(&rawClient{t: t, conn: conn})
	}
	return served
}

func closeCallbackTransports() []transport.Transport {
	return []transport.Transport{polling.Default, websocket.Default}
}

// A callback that calls Server.Close blocks the connection that runs it, and
// Close waits for that connection: the call returns the timeout error with that
// connection still open. Once the callback returns, the shutdown completes.
func TestServerCloseFromCallbackWaitsForItsConnection(t *testing.T) {
	for _, tr := range closeCallbackTransports() {
		for _, tc := range closeCallbackCases() {
			t.Run(tr.Name()+"/"+tc.name, func(t *testing.T) {
				s := NewServer(WithEngineOptions(shortPingOptions()...))
				waits, result := make(chan time.Duration, 1), make(chan error, 1)
				var first atomic.Bool
				s.closeTimer = func(d time.Duration) *time.Timer {
					if first.Swap(true) {
						return time.NewTimer(d)
					}
					waits <- d
					// The length of this wait does not matter: the connection
					// cannot finish while its callback is blocked in Close.
					return time.NewTimer(50 * time.Millisecond)
				}
				served := closeCallbackServer(t, s, tc, tr, func() { result <- s.Close() })
				require.Equal(t, 10*time.Second, recv(t, waits, "Close wait"))
				err := recv(t, result, "Close in the callback")
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.ErrorContains(t, err, "with 1 connections still open")
				require.NoError(t, s.Close(), "Close after the callback returned")
				require.Zero(t, s.Count())
				require.NoError(t, recv(t, served, "Serve"))
			})
		}
	}
}

// Started on its own goroutine from a callback, Close waits for the callback to
// return and completes the shutdown without a timeout.
func TestServerCloseOnGoroutineFromCallback(t *testing.T) {
	for _, tr := range closeCallbackTransports() {
		for _, tc := range closeCallbackCases() {
			t.Run(tr.Name()+"/"+tc.name, func(t *testing.T) {
				s := NewServer(WithEngineOptions(shortPingOptions()...))
				result := make(chan error, 1)
				served := closeCallbackServer(t, s, tc, tr, func() { go func() { result <- s.Close() }() })
				// recv gives up before the ten-second wait of Close ends, so a Close
				// that waits for its own caller fails here.
				require.NoError(t, recv(t, result, "Close"))
				require.Zero(t, s.Count())
				require.Empty(t, s.Rooms("/"))
				require.NoError(t, recv(t, served, "Serve"))
			})
		}
	}
}
