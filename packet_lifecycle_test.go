package socketio

import (
	"encoding/json"
	"errors"
	gorilla "github.com/gorilla/websocket"
	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/stretchr/testify/require"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
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
			reason := "client namespace disconnect"
			if applicationClose {
				reason = "server namespace disconnect"
			}
			require.Equal(t, reason, first.reason)
			require.Equal(t, reason, last.reason)
		})
	}
}

// Covers K24
func TestOnDisconnectReason(t *testing.T) {
	for _, scenario := range []string{"CLOSE", "timeout", "reset", "Conn.Close", "Server.Close", "parse error", "mid-frame reset", "mid-frame timeout"} {
		t.Run(scenario, func(t *testing.T) {
			disconnected := make(chan string, 1)
			opts := []engineio.Option{engineio.WithPingInterval(100 * time.Millisecond), engineio.WithPingTimeout(50 * time.Millisecond)}
			s := NewServer(WithEngineOptions(opts...))
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
				want = "server namespace disconnect"
				require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte(`42["close"]`)))
			case "Server.Close":
				require.NoError(t, s.Close())
			case "parse error":
				require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("49")))
			case "mid-frame reset", "mid-frame timeout":
				writer, err := c.NextWriter(gorilla.TextMessage)
				require.NoError(t, err)
				// Flush several nonfinal websocket fragments, then fail while the parser
				// is reading the body of the already obtained Engine.IO MESSAGE reader.
				_, err = writer.Write([]byte(`42["event","partial` + strings.Repeat("x", 20000)))
				require.NoError(t, err)
				want = "ping timeout"
				if scenario == "mid-frame reset" {
					require.NoError(t, c.UnderlyingConn().Close())
					want = "transport error"
				}

			}
			require.Equal(t, want, recv(t, disconnected, "OnDisconnect reason"))
		})
	}
}

// Covers G9
// Covers G19
// Covers G21
func TestIgnoredBinaryEventsConsumeTheirAttachments(t *testing.T) {
	_, url := defectServer(t, func(s *Server) { s.OnEvent("/", "alive", func(Conn) string { return "alive" }) })
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	for _, packet := range []string{
		`51-/missing,["unknown",{"_placeholder":true,"num":0}]`,
		`51-["unknown",{"_placeholder":true,"num":0}]`,
		`61-99[{"_placeholder":true,"num":0}]`,
		`51-["disconnecting",{"_placeholder":true,"num":0}]`,
	} {
		c.send(packet)
		w, err := c.conn.NextWriter(eioparser.Binary)
		require.NoError(t, err)
		_, err = w.Write([]byte("attachment"))
		require.NoError(t, err)
		require.NoError(t, w.Close())
		c.send(`27["alive"]`)
		require.Equal(t, "37[\"alive\"]\n", c.read())
	}
}

// Covers B2
func TestWrongTypeArgumentKeepsConnection(t *testing.T) {
	for _, kind := range []string{"event", "binary event", "ack"} {
		t.Run(kind, func(t *testing.T) {
			_, url := defectServer(t, func(s *Server) {
				s.OnEvent("/", "typed", func(_ Conn, n int) int { return n + 1 })
				s.OnEvent("/", "request-ack", func(c Conn) { c.Emit("ask", func(int) { t.Error("invalid ack reached callback") }) })
				s.OnError("/", func(c Conn, err error) {
					var closed *engineio.CloseError
					if errors.Is(err, io.EOF) || errors.As(err, &closed) {
						return
					}
					var typed *json.UnmarshalTypeError
					if !errors.As(err, &typed) {
						t.Errorf("unexpected error: %v", err)
						return
					}
					c.Emit("bad-type")
				})
			})
			c := dialRaw(t, url)
			require.Equal(t, "0", c.read())
			switch kind {
			case "event":
				c.send(`2["typed","bad"]`)
			case "binary event":
				c.send(`51-["typed",{"_placeholder":true,"num":0}]`)
				w, err := c.conn.NextWriter(eioparser.Binary)
				require.NoError(t, err)
				_, err = io.WriteString(w, "bytes")
				require.NoError(t, err)
				require.NoError(t, w.Close())
			case "ack":
				c.send(`2["request-ack"]`)
				pkt := c.read()
				idx := strings.IndexByte(pkt, '[')
				require.Greater(t, idx, 1)
				id, err := strconv.ParseUint(pkt[1:idx], 10, 64)
				require.NoError(t, err)
				c.send("3" + strconv.FormatUint(id, 10) + `["bad"]`)
			}
			require.Equal(t, "2[\"bad-type\"]\n", c.read())
			c.send(`27["typed",41]`)
			require.Equal(t, "37[42]\n", c.read(), "a valid packet following a type mismatch must still be handled")
		})
	}
}
