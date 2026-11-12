package socketio

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/sshaplygin/go-socket.io/parser"
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
			// The server can still be writing CONNECT: the drop fails a read or a write.
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

type limitNotice struct{ File parser.Buffer }

// A binary attachment past the limit of the receiving role ends its session with transport
// error and one WARN record of its Engine.IO layer, whether a reflected or a typed handler
// decodes it; OnError is not called.
func TestMessageLimitEndsSession(t *testing.T) {
	for _, role := range []string{"server", "client"} {
		for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
			for _, typed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/typed=%t", role, tr.Name(), typed), func(t *testing.T) {
					rec := newAttrRecorder()
					setDefault(t, rec) // the Engine.IO client logs through slog.Default
					// A heartbeat that a loaded host keeps, so only the limit ends the session.
					eopts := append(shortPingOptions(), engineio.WithLogger(slog.New(rec)), engineio.WithPingInterval(time.Second), engineio.WithPingTimeout(5*time.Second))
					copts := []client.Option{client.WithTransports(tr), client.WithReconnection(false)}
					if role == "server" {
						eopts = append(eopts, engineio.WithMaxHTTPBufferSize(512))
					} else {
						copts = append(copts, client.WithMaxMessageSize(512))
					}
					srv := NewServer(WithEngineOptions(eopts...))
					conns, errs, reasons := make(chan Conn, 1), make(chan error, 8), make(chan string, 1)
					srv.OnConnect("/", func(c Conn) error { conns <- c; return nil })
					srv.OnError("/", func(_ Conn, err error) { errs <- err })
					go func() { _ = srv.Serve() }()
					ts := httptest.NewServer(srv)
					t.Cleanup(func() { _ = srv.Close(); ts.Close() })
					cl, err := client.New(ts.URL, copts...)
					require.NoError(t, err)
					t.Cleanup(func() { _ = cl.Close() })
					cl.OnError(func(_ client.Conn, err error) { errs <- err })
					if typed {
						notice := NewEvent[limitNotice]("big")
						require.NoError(t, notice.Handle(srv.Of("/"), func(context.Context, Conn, limitNotice) error { return nil }))
						require.NoError(t, notice.HandleClient(cl, func(context.Context, Endpoint, limitNotice) error { return nil }))
					} else {
						srv.OnEvent("/", "big", func(Conn, parser.Buffer) {})
						cl.OnEvent("big", func(client.Conn, parser.Buffer) {})
					}
					if role == "server" {
						srv.OnDisconnect("/", func(_ Conn, reason string) { reasons <- reason })
					} else {
						cl.OnDisconnect(func(_ client.Conn, reason string) { reasons <- reason })
					}
					require.NoError(t, cl.Connect())
					c, big := recv(t, conns, "OnConnect"), &parser.Buffer{Data: make([]byte, 2048)}
					if role == "server" {
						cl.Emit("big", big)
					} else {
						c.Emit("big", big)
					}
					require.Equal(t, "transport error", recv(t, reasons, "OnDisconnect"))
					if role == "server" {
						recv(t, serverCompletion(srv, c.ID()), "the end of the connection")
					} // a Go client reports to OnError before OnDisconnect
					require.Zero(t, len(errs), "OnError was called")
					tooLarge := func() []map[string]string { return only(rec.since(0), "engineio: message too large") }
					require.Eventually(t, func() bool { return len(tooLarge()) > 0 }, waitFor, time.Millisecond)
					require.Len(t, tooLarge(), 1, "one record per violation")
					require.Equal(t, tr.Name(), tooLarge()[0]["transport"])
					require.Contains(t, tooLarge()[0]["err"], "limit 512 bytes")
				})
			}
		}
	}
}

// writeFault, once armed, fails the Close of every frame writer and its Write with err.
type writeFault struct {
	*fakeConn
	err   error
	armed *atomic.Bool
}

func (f writeFault) NextWriter(ft eioparser.FrameType) (io.WriteCloser, error) {
	w, err := f.fakeConn.NextWriter(ft)
	if err != nil || !f.armed.Load() {
		return w, err
	}
	return failingFrame{w, f.err}, nil
}

type failingFrame struct {
	io.WriteCloser
	err error
}

func (w failingFrame) Write(p []byte) (int, error) { return len(p), w.err }
func (w failingFrame) Close() error                { return io.ErrClosedPipe }

// A transport failure while writing an emit ends the session: OnDisconnect reports it
// and OnError is not called.
func TestWriteFailureEndsSession(t *testing.T) {
	for mode, err := range map[string]error{"NextWriter": nil, "frame Write": io.ErrUnexpectedEOF, "frame Close": nil} {
		t.Run(mode, func(t *testing.T) {
			p, armed := newPeer(t, 'S', hooks{}), new(atomic.Bool)
			p.srv.serveConn(writeFault{p.fc, err, armed})
			p.nc = recv(t, p.conns, "root OnConnect")
			require.Equal(t, "0", recv(t, p.fc.out, "root CONNECT"))
			armed.Store(mode != "NextWriter")
			p.fc.failWrite.Store(mode == "NextWriter")
			p.nc.Emit("big", strings.Repeat("x", 1<<16))
			p.disconnected(t, "/")
			require.Equal(t, "forced close", recv(t, p.reasons, "OnDisconnect reason"))
			require.Empty(t, drain(p.errs), "OnError was called")
		})
	}
}
