package socketio

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

// partialBody starts a 20-character packet that the client never completes.
const partialBody = `20:42["x","`

// deadlineConfig configures a server for the polling request deadline tests.
type deadlineConfig struct {
	heartbeat   time.Duration // PingInterval and PingTimeout; 1 s by default
	opaque      bool          // hide the ResponseWriter's deadline support (no Unwrap)
	readTimeout time.Duration // http.Server.ReadTimeout
	sendBuffer  int           // SetWriteBuffer of accepted connections
}

// deadlineServer serves a root namespace that reports its connection, every
// "tick" event, every POST that reaches the handler and the disconnect reason.
type deadlineServer struct {
	srv     *Server
	ts      *httptest.Server
	conns   chan Conn
	ticks   chan struct{}
	posts   chan struct{}
	reasons chan string
}

// opaqueWriter exposes only the http.ResponseWriter methods, so
// http.ResponseController reports http.ErrNotSupported for deadlines.
type opaqueWriter struct{ http.ResponseWriter }

// sendBufferListener sets a small send buffer on every accepted connection.
type sendBufferListener struct {
	net.Listener
	size int
}

func (l sendBufferListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetWriteBuffer(l.size)
	}
	return conn, err
}

func newDeadlineServer(t *testing.T, cfg deadlineConfig) *deadlineServer {
	t.Helper()
	if cfg.heartbeat == 0 {
		cfg.heartbeat = time.Second
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &deadlineServer{conns: make(chan Conn, 1), ticks: make(chan struct{}, 8),
		posts: make(chan struct{}, 8), reasons: make(chan string, 1)}
	d.srv = NewServer(WithLogger(quiet), WithEngineOptions(engineio.WithLogger(quiet),
		engineio.WithPingInterval(cfg.heartbeat), engineio.WithPingTimeout(cfg.heartbeat),
		engineio.WithHTTPCompression(false)))
	d.srv.OnConnect("/", func(c Conn) error { d.conns <- c; return nil })
	d.srv.OnEvent("/", "tick", func(Conn) { d.ticks <- struct{}{} })
	d.srv.OnDisconnect("/", func(_ Conn, reason string) { d.reasons <- reason })
	go func() { _ = d.srv.Serve() }()
	d.ts = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			d.posts <- struct{}{}
		}
		if cfg.opaque {
			w = opaqueWriter{w}
		}
		d.srv.ServeHTTP(w, r)
	}))
	d.ts.Config.ReadTimeout = cfg.readTimeout
	if cfg.sendBuffer > 0 {
		d.ts.Listener = sendBufferListener{d.ts.Listener, cfg.sendBuffer}
	}
	d.ts.Start()
	t.Cleanup(func() { _ = d.srv.Close(); d.ts.Close() })
	return d
}

func (d *deadlineServer) url(sid string) string {
	return d.ts.URL + "/socket.io/?EIO=3&b64=1&transport=polling" + sid
}

// open completes a b64 polling handshake and returns the session ID and its connection.
func (d *deadlineServer) open(t *testing.T) (string, Conn) {
	t.Helper()
	resp, err := http.Get(d.url(""))
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.NoError(t, err)
	s := string(body)
	start, end := strings.Index(s, "{"), strings.Index(s, "}")
	require.True(t, start >= 0 && end > start, "handshake response %q", s)
	var open struct{ Sid string }
	require.NoError(t, json.Unmarshal([]byte(s[start:end+1]), &open))
	return open.Sid, recv(t, d.conns, "OnConnect")
}

// tick posts one complete packet and returns once the server has handled it.
func (d *deadlineServer) tick(t *testing.T, sid string) time.Time {
	t.Helper()
	resp, err := http.Post(d.url("&sid="+sid), "text/plain;charset=UTF-8", strings.NewReader(`10:42["tick"]`))
	require.NoError(t, err)
	_ = resp.Body.Close()
	recv(t, d.posts, "complete POST")
	recv(t, d.ticks, "tick")
	return time.Now()
}

// dial opens a raw TCP connection with a 4 KiB receive buffer and sends one
// polling request head for sid; extra adds request headers.
func (d *deadlineServer) dial(t *testing.T, method, sid, extra string) net.Conn {
	t.Helper()
	raw, err := net.Dial("tcp", d.ts.Listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, raw.(*net.TCPConn).SetReadBuffer(4096))
	_, err = fmt.Fprintf(raw, "%s /socket.io/?EIO=3&b64=1&transport=polling&sid=%s HTTP/1.1\r\nHost: x\r\n%s\r\n", method, sid, extra)
	require.NoError(t, err)
	return raw
}

// partialPost sends a POST that declares 100 body bytes, delivers 11 and idles.
func (d *deadlineServer) partialPost(t *testing.T, sid string) net.Conn {
	t.Helper()
	raw := d.dial(t, http.MethodPost, sid, "Content-Type: text/plain;charset=UTF-8\r\nContent-Length: 100\r\n")
	_, err := io.WriteString(raw, partialBody)
	require.NoError(t, err)
	recv(t, d.posts, "partial POST")
	time.Sleep(100 * time.Millisecond) // let the server start reading the body
	return raw
}

// within receives from ch before deadline.
func within[T any](t *testing.T, ch <-chan T, deadline time.Time, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(time.Until(deadline)):
		t.Fatalf("%s did not happen in time", what)
		var zero T
		return zero
	}
}

// requireBaseline waits up to limit for the library goroutine count to return to baseline.
func requireBaseline(t *testing.T, baseline int, limit time.Duration) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for leaks := leakedModuleStacks(); len(leaks) > baseline; leaks = leakedModuleStacks() {
		if time.Now().After(deadline) {
			t.Fatalf("%d library goroutines remain after %v (baseline %d):\n%s",
				len(leaks), limit, baseline, strings.Join(leaks, "\n\n"))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A POST body that never completes does not keep its session open: the
// heartbeat, EngineConn().Close() and Server.Close each end it. Without
// deadline support only the handler goroutine waits for the peer, and closing
// the client socket releases it.
func TestPollingPartialPostEndsSession(t *testing.T) {
	for _, opaque := range []bool{false, true} {
		name := map[bool]string{false: "deadlines", true: "no deadline support"}[opaque]
		t.Run(name, func(t *testing.T) {
			// release checks Server.Close, releases a handler that has no deadline
			// support by closing the client socket, and checks the leak gate.
			release := func(t *testing.T, d *deadlineServer, raw net.Conn, baseline int) {
				start := time.Now()
				_ = d.srv.Close()
				require.Less(t, time.Since(start), 2*time.Second, "Server.Close duration")
				if opaque {
					_ = raw.Close()
				}
				requireBaseline(t, baseline, 2*time.Second)
			}
			t.Run("ping timeout", func(t *testing.T) {
				baseline := len(leakedModuleStacks())
				d := newDeadlineServer(t, deadlineConfig{opaque: opaque})
				sid, _ := d.open(t)
				last := d.tick(t, sid)
				raw := d.partialPost(t, sid)
				require.Equal(t, "ping timeout", within(t, d.reasons, last.Add(3*time.Second), "OnDisconnect"))
				release(t, d, raw, baseline)
			})
			t.Run("EngineConn close", func(t *testing.T) {
				baseline := len(leakedModuleStacks())
				d := newDeadlineServer(t, deadlineConfig{opaque: opaque})
				sid, c := d.open(t)
				d.tick(t, sid)
				raw := d.partialPost(t, sid)
				// A client keeps a poll pending; the orderly close sends CLOSE through it.
				poll := make(chan string, 1)
				go func() {
					resp, err := http.Get(d.url("&sid=" + sid))
					if err != nil {
						poll <- err.Error()
						return
					}
					body, _ := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					poll <- string(body)
				}()
				start := time.Now()
				require.NoError(t, c.EngineConn().Close())
				require.Equal(t, "forced close", within(t, d.reasons, start.Add(time.Second), "OnDisconnect"))
				require.Contains(t, recv(t, poll, "poll"), "1:1", "the poll carries CLOSE")
				release(t, d, raw, baseline)
			})
			t.Run("Server.Close", func(t *testing.T) {
				baseline := len(leakedModuleStacks())
				d := newDeadlineServer(t, deadlineConfig{heartbeat: time.Hour, opaque: opaque})
				sid, _ := d.open(t)
				d.tick(t, sid)
				raw := d.partialPost(t, sid)
				release(t, d, raw, baseline)
				recv(t, d.reasons, "OnDisconnect")
			})
		})
	}
}

// http.Server.ReadTimeout still ends a partial POST when it is shorter than
// the heartbeat; the session deadline never extends it.
func TestPollingPartialPostServerReadTimeout(t *testing.T) {
	d := newDeadlineServer(t, deadlineConfig{heartbeat: time.Hour, readTimeout: 500 * time.Millisecond})
	sid, _ := d.open(t)
	start := time.Now()
	d.partialPost(t, sid)
	within(t, d.reasons, start.Add(2*time.Second), "OnDisconnect")
}

// A poll whose client never reads a payload larger than the socket buffers
// keeps no library goroutine once the session has closed. The server's send
// buffer and the client's receive buffer are 4 KiB on every OS.
func TestPollingStalledGetIsReleased(t *testing.T) {
	baseline := len(leakedModuleStacks())
	d := newDeadlineServer(t, deadlineConfig{sendBuffer: 4096})
	sid, c := d.open(t)
	raw := d.dial(t, http.MethodGet, sid, "")
	c.Emit("big", strings.Repeat("x", 1_000_000))
	status := make([]byte, len("HTTP/1.1 200"))
	_, err := io.ReadFull(raw, status)
	require.NoError(t, err)
	require.Equal(t, "HTTP/1.1 200", string(status), "the poll response has started")
	require.Equal(t, "ping timeout", recv(t, d.reasons, "OnDisconnect"))
	// Server.Close stops Serve, which the baseline does not include; it does
	// not release a handler blocked in its response write.
	_ = d.srv.Close()
	requireBaseline(t, baseline, 2*time.Second)
}
