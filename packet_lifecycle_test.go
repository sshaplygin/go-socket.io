package socketio

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

// Covers S10
func TestUnknownNamespaceKeepsConnection(t *testing.T) {
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnEvent("/", "echo", func(_ Conn, n int) int { return n })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send("0/missing,")
	require.Equal(t, "4/missing,\"Invalid namespace\"\n", c.read())
	c.send(`27["echo",42]`)
	require.Equal(t, "37[42]\n", c.read())
}

// Covers N9
func TestNamespaceConnectPacket(t *testing.T) {
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnConnect("/chat", func(Conn) error { return nil })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
}

// Covers K22
func TestClientErrorPacket(t *testing.T) {
	errs := make(chan error, 8)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnError("/", func(_ Conn, err error) { errs <- err })
		s.OnEvent("/", "echo", func(_ Conn, n int) int { return n })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	for _, value := range []string{`"denied"`, `{"message":"denied"}`, `["detail"]`, `null`} {
		c.send("4" + value)
		want := value
		if value == `"denied"` {
			want = "denied"
		}
		require.EqualError(t, recv(t, errs, "OnError"), want)
	}
	c.send(`27["echo",42]`)
	require.Equal(t, "37[42]\n", c.read())
}

// Covers R10
func TestEncodeErrorKeepsConnection(t *testing.T) {
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnEvent("/", "bad", func(c Conn) { c.Emit("bad", make(chan int)); c.Emit("next", 42) })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send(`2["bad"]`)
	require.Equal(t, "4\"encode error\"\n", c.read())
	require.Equal(t, "2[\"next\",42]\n", c.read())
}

// Covers K23
func TestDisconnectingRooms(t *testing.T) {
	for _, applicationClose := range []bool{false, true} {
		t.Run(map[bool]string{false: "client", true: "application"}[applicationClose], func(t *testing.T) {
			type observation struct {
				rooms  []string
				reason string
			}
			before, after := make(chan observation, 1), make(chan observation, 1)
			ts := newTestServer(t, func(s *Server) {
				s.OnConnect("/", func(c Conn) error { c.Join("lobby"); return nil })
				s.OnEvent("/", "close", func(c Conn) { _ = c.Close() })
				s.OnEvent("/", "disconnecting", func(c Conn, reason string) { before <- observation{c.Rooms(), reason} })
				s.OnDisconnect("/", func(c Conn, reason string) { after <- observation{c.Rooms(), reason} })
			})
			c := dialRaw(t, ts.URL)
			require.Equal(t, "0", c.read())
			if applicationClose {
				c.send(`2["close"]`)
			} else {
				c.send("1")
			}
			first, last := recv(t, before, "disconnecting"), recv(t, after, "OnDisconnect")
			require.Contains(t, first.rooms, "lobby")
			require.Empty(t, last.rooms)
			reason := clientDisconnectMsg
			if applicationClose {
				reason = "forced close"
			}
			require.Equal(t, reason, first.reason)
			require.Equal(t, reason, last.reason)
		})
	}
}

// Covers K24
func TestOnDisconnectReason(t *testing.T) {
	for _, scenario := range []string{"CLOSE", "timeout", "reset", "Conn.Close", "Server.Close", "parse error"} {
		t.Run(scenario, func(t *testing.T) {
			disconnected := make(chan string, 1)
			opts := &engineio.Options{PingInterval: 100 * time.Millisecond, PingTimeout: 50 * time.Millisecond}
			s := NewServer(opts)
			s.OnConnect("/", func(Conn) error { return nil })
			s.OnEvent("/", "close", func(c Conn) { _ = c.Close() })
			s.OnDisconnect("/", func(_ Conn, reason string) { disconnected <- reason })
			s.OnError("/", func(Conn, error) {})
			go func() { _ = s.Serve() }()
			ts := httptest.NewServer(s)
			defer ts.Close()
			defer func() { _ = s.Close() }()
			c, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
			require.NoError(t, err)
			defer func() { _ = c.Close() }()
			// Consume Engine.IO OPEN and Socket.IO CONNECT before the trigger.
			_, _, err = c.ReadMessage()
			require.NoError(t, err)
			_, data, err := c.ReadMessage()
			require.NoError(t, err)
			require.Equal(t, "40", string(data))
			want := "forced close"
			switch scenario {
			case "CLOSE":
				require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("1")))
				want = "transport close"
			case "timeout":
				want = "ping timeout"
			case "reset":
				require.NoError(t, c.UnderlyingConn().Close())
				want = "transport error"
			case "Conn.Close":
				require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte(`42["close"]`)))
			case "Server.Close":
				require.NoError(t, s.Close())
			case "parse error":
				require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("49")))
			}
			require.Equal(t, want, recv(t, disconnected, "OnDisconnect reason"))
		})
	}
}
