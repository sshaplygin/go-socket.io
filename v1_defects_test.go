package socketio

import (
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

func defectServer(t *testing.T, configure func(*Server)) (*Server, string) {
	t.Helper()
	s := NewServer(&Options{Engine: shortPingOptions()})
	s.OnConnect("/", func(Conn) error { return nil })
	configure(s)
	go func() { _ = s.Serve() }()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	return s, ts.URL
}

// Covers B1
func TestEmitNil(t *testing.T) {
	_, url := defectServer(t, func(s *Server) {
		s.OnEvent("/", "get-nil", func(c Conn) { c.Emit("nil", nil); c.Emit("pair", nil, "tail") })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send(`2["get-nil"]`)
	require.Equal(t, "2[\"nil\",null]\n", c.read())
	require.Equal(t, "2[\"pair\",null,\"tail\"]\n", c.read())
}

// Covers S12
func TestNamespaceBroadcastOnce(t *testing.T) {
	connected := make(chan Conn, 2)
	s, url := defectServer(t, func(s *Server) {
		s.OnConnect("/", func(c Conn) error { c.Join("one"); c.Join("two"); connected <- c; return nil })
	})
	a, b := dialRaw(t, url), dialRaw(t, url)
	require.Equal(t, "0", a.read())
	require.Equal(t, "0", b.read())
	ca, cb := recv(t, connected, "first connection"), recv(t, connected, "second connection")
	require.True(t, s.BroadcastToNamespace("/", "news", "once"))
	ca.Emit("after")
	cb.Emit("after")
	for _, c := range []*rawClient{a, b} {
		require.Equal(t, "2[\"news\",\"once\"]\n", c.read())
		require.Equal(t, "2[\"after\"]\n", c.read(), "a broadcast must not be repeated for each room")
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
