package socketio

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// rawWS is a raw Engine.IO 3 websocket client of a test server.
type rawWS struct{ ws *gorilla.Conn }

// dialRawWS connects to the server at url and waits for the CONNECT of the root namespace.
func dialRawWS(t *testing.T, url string) *rawWS {
	t.Helper()
	ws, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(url, "http")+"/socket.io/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(waitFor)))
	r := &rawWS{ws: ws}
	r.next(t, "40")
	return r
}

func (r *rawWS) read() (string, error) {
	_, b, err := r.ws.ReadMessage()
	return strings.TrimSpace(string(b)), err
}

// next returns the first message that starts with prefix.
func (r *rawWS) next(t *testing.T, prefix string) string {
	t.Helper()
	for {
		m, err := r.read()
		require.NoError(t, err)
		if strings.HasPrefix(m, prefix) {
			return m
		}
	}
}

func (r *rawWS) send(t *testing.T, msg string) {
	t.Helper()
	require.NoError(t, r.ws.WriteMessage(gorilla.TextMessage, []byte(msg)))
}

// rawDecodeServer is a server with handlers whose parameters fail to decode in
// different ways, and a raw websocket client connected to its root namespace.
type rawDecodeServer struct {
	*rawWS
	srv     *Server
	conns   chan Conn
	errs    chan error
	reasons chan string
}

// newRawDecodeServer registers further handlers through setup before the client connects.
func newRawDecodeServer(t *testing.T, setup ...func(*Server)) *rawDecodeServer {
	t.Helper()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(WithEngineOptions(engineio.WithLogger(quiet)))
	s := &rawDecodeServer{srv: srv, conns: make(chan Conn, 1), errs: make(chan error, 4), reasons: make(chan string, 1)}
	srv.OnConnect("/", func(c Conn) error { s.conns <- c; return nil })
	srv.OnEvent("/", "num", func(_ Conn, n int) int { return n })
	srv.OnEvent("/", "time", func(_ Conn, ts time.Time) string { return ts.String() })
	srv.OnEvent("/", "ping", func(Conn) string { return "pong" })
	srv.OnError("/", func(c Conn, err error) {
		if !c.Connected() {
			err = fmt.Errorf("OnError ran after the close: %w", err)
		}
		c.Emit("reported", err.Error())
		s.errs <- err
	})
	srv.OnDisconnect("/", func(_ Conn, reason string) { s.reasons <- reason })
	for _, f := range setup {
		f(srv)
	}
	go func() { _ = srv.Serve() }()
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { _ = srv.Close(); ts.Close() })
	s.rawWS = dialRawWS(t, ts.URL)
	return s
}

// ackRequest waits for the server's acknowledged event name, such as 421["q"], and
// returns its acknowledgement id.
func (s *rawDecodeServer) ackRequest(t *testing.T, name string) string {
	t.Helper()
	for {
		m := s.next(t, "42")
		if id, ok := strings.CutSuffix(strings.TrimPrefix(m, "42"), `["`+name+`"]`); ok {
			require.NotEmpty(t, id, "the event %s requests no acknowledgement", m)
			return id
		}
	}
}

// closed reads until the server has closed the websocket.
func (s *rawDecodeServer) closed(t *testing.T) {
	t.Helper()
	for {
		if _, err := s.read(); err != nil {
			require.False(t, errors.Is(err, os.ErrDeadlineExceeded), "the websocket stayed open")
			return
		}
	}
}

// An argument of the wrong JSON type for a handler's parameter is reported to OnError
// and keeps the connection: a requested ACK is answered with the error ACK, an Emit from
// OnError is delivered and a later event is answered.
func TestArgumentTypeMismatchKeepsConnection(t *testing.T) {
	s := newRawDecodeServer(t)
	s.send(t, `421["num","x"]`)
	require.ErrorContains(t, recv(t, s.errs, "the decode error"), "decode argument 0")
	require.Equal(t, `431[{"code":"invalid_payload","message":"decode argument 0: json: cannot unmarshal string into Go value of type int"}]`, s.next(t, "431"))
	require.Equal(t, `42["reported","decode argument 0: json: cannot unmarshal string into Go value of type int"]`, s.next(t, `42["reported"`))
	s.send(t, `422["ping"]`)
	require.Equal(t, `432["pong"]`, s.next(t, "432"))
	require.Empty(t, drain(s.reasons), "the connection ended")
}

// A packet that cannot be decoded, and an argument that fails to decode with an error
// other than a JSON type mismatch, such as the UnmarshalJSON error of a time.Time
// parameter, are reported to OnError on a connection that is still connected; then the
// library closes it with forced close and sends no ACK. The close discards the queue, so
// an Emit from that OnError call may not be written.
func TestUndecodablePacketReportedBeforeClose(t *testing.T) {
	for _, tc := range []struct{ name, packet, err string }{
		{"invalid payload", `421{"a":1}`, "invalid payload"},
		{"argument decode error", `421["time","bad"]`, "decode argument 0: parsing time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newRawDecodeServer(t)
			s.send(t, tc.packet)
			err := recv(t, s.errs, "OnError")
			require.ErrorContains(t, err, tc.err)
			require.NotContains(t, err.Error(), "after the close")
			require.Equal(t, "forced close", recv(t, s.reasons, "OnDisconnect"))
			for { // no ACK precedes the close of the websocket
				m, err := s.read()
				if err != nil {
					require.False(t, errors.Is(err, os.ErrDeadlineExceeded), "the websocket stayed open")
					break
				}
				require.False(t, strings.HasPrefix(m, "43"), "an ACK was sent: %s", m)
			}
		})
	}
}

// The Go client treats argument decode errors as the server does: a JSON type mismatch
// keeps the connection, any other argument decode error is reported to OnError and ends
// the connection with forced close.
func TestClientArgumentDecodeErrors(t *testing.T) {
	p := newHeartbeatPair(t, websocket.Default, 25*time.Second, 20*time.Second, func(p *heartbeatPair) {
		p.cl.OnEvent("num", func(_ client.Conn, n int) int { return n })
		p.cl.OnEvent("time", func(_ client.Conn, ts time.Time) string { return ts.String() })
		p.cl.OnEvent("ping", func(client.Conn) string { return "pong" })
	})
	c := recv(t, p.conns, "OnConnect")
	acks := make(chan string, 2)

	c.Emit("num", "x")
	require.ErrorContains(t, recv(t, p.errs, "the type mismatch"), "decode argument 0: json: cannot unmarshal string")
	c.Emit("ping", func(s string) { acks <- s })
	require.Equal(t, "pong", recv(t, acks, "the ACK after the type mismatch"))

	c.Emit("time", "bad")
	require.ErrorContains(t, recv(t, p.errs, "the decode error"), "decode argument 0: parsing time")
	require.Equal(t, "forced close", recv(t, p.clientReason, "the client's OnDisconnect"))
	recv(t, p.serverReasons, "the server's OnDisconnect")
	require.Empty(t, drain(p.errs), "OnError was called again")
}

// An AckFunc called after its namespace connection ended writes nothing, although the
// Engine.IO connection is still open: a packet written after it arrives, and no ACK
// precedes it. An AckFunc of the same connection called while it is current answers.
func TestAckFuncAfterDisconnectSendsNothing(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(WithEngineOptions(engineio.WithLogger(quiet)))
	roots, acks, ended := make(chan Conn, 1), make(chan AckFunc, 2), make(chan string, 1)
	srv.OnConnect("/", func(c Conn) error { roots <- c; return nil })
	srv.OnConnect("/chat", func(Conn) error { return nil })
	srv.OnEvent("/chat", "later", func(_ Conn, ack AckFunc) { acks <- ack })
	srv.OnDisconnect("/chat", func(_ Conn, reason string) { ended <- reason })
	go func() { _ = srv.Serve() }()
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { _ = srv.Close(); ts.Close() })

	r := dialRawWS(t, ts.URL)
	root := recv(t, roots, "the root OnConnect")
	r.send(t, "40/chat")
	r.next(t, "40/chat")
	r.send(t, `42/chat,1["later"]`)
	r.send(t, `42/chat,2["later"]`)
	late := recv(t, acks, "the first handler call")
	recv(t, acks, "the second handler call")("now")
	require.Equal(t, `43/chat,2["now"]`, r.next(t, "43"))

	r.send(t, "41/chat")
	require.Equal(t, "client namespace disconnect", recv(t, ended, "the /chat OnDisconnect"))
	late("late") // the namespace connection has ended
	// The packets of an Engine.IO connection share one queue, so an ACK admitted by the
	// call above would arrive before the marker.
	root.Emit("marker")
	for {
		m := r.next(t, "4")
		require.False(t, strings.HasPrefix(m, "43"), "the late AckFunc wrote %s", m)
		if m == `42["marker"]` {
			break
		}
	}
}
