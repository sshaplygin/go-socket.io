package socketio

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// goClientClose connects a Go client over trs, waits for its final transport and closes it.
func goClientClose(trs ...transport.Transport) func(*testing.T, *deadlineServer) Conn {
	return func(t *testing.T, d *deadlineServer) Conn {
		cl, err := client.New(d.ts.URL, client.WithTransports(trs...), client.WithReconnection(false))
		require.NoError(t, err)
		require.NoError(t, cl.Connect())
		c := recv(t, d.conns, "OnConnect")
		want := trs[len(trs)-1].Name()
		require.Eventually(t, func() bool { return c.EngineConn().Transport() == want }, waitFor, time.Millisecond)
		require.NoError(t, cl.Close())
		return c
	}
}

// The end of an Engine.IO session reaches OnDisconnect with its reason, and OnError is
// not called, whichever side or failure ends it.
func TestSessionEndSkipsOnError(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		heartbeat    time.Duration
		end          func(*testing.T, *deadlineServer) Conn
	}{
		{"client close/polling", "client namespace disconnect", 0, goClientClose(polling.Default)},
		{"client close/websocket", "client namespace disconnect", 0, goClientClose(websocket.Default)},
		{"client close/upgrade", "client namespace disconnect", 0, goClientClose(polling.Default, websocket.Default)},
		{"abrupt drop/websocket", "transport error", 0, func(t *testing.T, d *deadlineServer) Conn {
			ws, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(d.ts.URL, "http")+"/socket.io/?EIO=3&transport=websocket", nil)
			require.NoError(t, err)
			c := recv(t, d.conns, "OnConnect")
			// Drop after the CONNECT packet, so that the server is not writing.
			for msg := ""; msg != "40"; {
				_, b, err := ws.ReadMessage()
				require.NoError(t, err)
				msg = string(b)
			}
			require.NoError(t, ws.NetConn().Close()) // no close frame
			return c
		}},
		{"abrupt drop/polling", "transport error", 0, func(t *testing.T, d *deadlineServer) Conn {
			sid, c := d.open(t)
			require.NoError(t, d.dial(t, http.MethodGet, sid, "").Close()) // a pending poll's connection
			return c
		}},
		{"ping timeout", "ping timeout", 200 * time.Millisecond, func(t *testing.T, d *deadlineServer) Conn {
			_, c := d.open(t)
			return c
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDeadlineServer(t, deadlineConfig{heartbeat: tc.heartbeat})
			errs := make(chan error, 8)
			d.srv.OnError("/", func(_ Conn, err error) { errs <- err })
			c := tc.end(t, d)
			require.Equal(t, tc.reason, recv(t, d.reasons, "OnDisconnect"))
			recv(t, serverCompletion(d.srv, c.ID()), "the end of the connection")
			require.Empty(t, drain(errs), "OnError was called")
		})
	}
}

// A message past the server's limit ends the session with transport error and a WARN
// record instead of an OnError call.
func TestMessageLimitEndsSession(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			rec := newAttrRecorder()
			srv := NewServer(WithLogger(slog.New(rec)), WithEngineOptions(append(shortPingOptions(), engineio.WithMaxHTTPBufferSize(512))...))
			conns, errs, reasons := make(chan Conn, 1), make(chan error, 8), make(chan string, 1)
			srv.OnConnect("/", func(c Conn) error { conns <- c; return nil })
			srv.OnError("/", func(_ Conn, err error) { errs <- err })
			srv.OnDisconnect("/", func(_ Conn, reason string) { reasons <- reason })
			go func() { _ = srv.Serve() }()
			ts := httptest.NewServer(srv)
			t.Cleanup(func() { _ = srv.Close(); ts.Close() })
			cl, err := client.New(ts.URL, client.WithTransports(tr), client.WithReconnection(false))
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			require.NoError(t, cl.Connect())
			c := recv(t, conns, "OnConnect")
			cl.Emit("big", strings.Repeat("x", 2048))
			require.Equal(t, "transport error", recv(t, reasons, "OnDisconnect"))
			recv(t, serverCompletion(srv, c.ID()), "the end of the connection")
			require.Empty(t, drain(errs), "OnError was called")
			warns := loud(rec.since(0), "")
			require.Len(t, warns, 1)
			require.Equal(t, "socketio: message too large", warns[0]["msg"])
			require.Equal(t, c.ID(), warns[0]["sid"])
		})
	}
}
