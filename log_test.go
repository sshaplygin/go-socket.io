package socketio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/sshaplygin/go-socket.io/logger"
)

// captureLogs sends the records of opts.Logger and of slog.Default to one recorder. It
// must not run in parallel; Cleanup restores the default logger.
func captureLogs(t *testing.T, opts *engineio.Options) *attrRecorder {
	rec := newAttrRecorder()
	setDefault(t, rec)
	opts.Logger = slog.New(rec)
	return rec
}

// setDefault makes h the default handler until Cleanup; the test must not run in parallel.
func setDefault(t *testing.T, h slog.Handler) {
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev); log.SetOutput(prevOut); log.SetFlags(prevFlags) })
}

// byNsp maps the nsp of each record msg to the value of its attribute key (nil if absent);
// a second record of msg for one nsp, or one at a level other than lvl, fails t.
func (h *recordingHandler) byNsp(t *testing.T, msg, key string, lvl slog.Level) map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]any{}
	for _, r := range h.recs {
		m := map[string]slog.Value{}
		r.Attrs(func(a slog.Attr) bool { m[a.Key] = a.Value; return true })
		if r.Message == msg {
			assert.NotContains(t, out, m["nsp"].String(), "a second %q record", msg)
			assert.Equal(t, lvl, r.Level, msg)
			out[m["nsp"].String()] = m[key].Any()
		}
	}
	return out
}

// since returns the records kept after the first n.
func (h *attrRecorder) since(n int) []map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]map[string]string(nil), (*h.recs)[n:]...)
}

func (h *attrRecorder) len() int { return len(h.since(0)) }

// loud returns the records that the module's code logged above DEBUG, other than msg.
// Function names escape the dot of the root package path as %2e.
func loud(recs []map[string]string, msg string) (out []map[string]string) {
	for _, m := range recs {
		fn := strings.ReplaceAll(m["func"], "%2e", ".")
		if strings.HasPrefix(fn, "github.com/sshaplygin/go-socket.io") && m["msg"] != msg &&
			m["level"] != slog.LevelDebug.String() && m["level"] != "DEBUG-4" {
			out = append(out, m)
		}
	}
	return out
}

func only(recs []map[string]string, msg string) (out []map[string]string) {
	for _, m := range recs {
		if m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

// faultyTransport accepts connections whose writes fail, so that InitSession fails.
type faultyTransport struct{ transport.Transport }

type faultyConn struct{ transport.Conn }

func (faultyTransport) Name() string { return "faulty" }
func (faultyTransport) Accept(http.ResponseWriter, *http.Request) (transport.Conn, error) {
	return faultyConn{}, nil
}
func (faultyConn) SetReadDeadline(time.Time) error  { return nil }
func (faultyConn) SetWriteDeadline(time.Time) error { return nil }
func (faultyConn) Close() error                     { return nil }
func (faultyConn) RemoteAddr() net.Addr             { return nil }
func (faultyConn) NextWriter(frame.Type, packet.Type) (io.WriteCloser, error) {
	return nil, errors.New("write failed")
}

// Covers 1L-T9 (S).
func TestRequestRejectedRecords(t *testing.T) {
	opts := &engineio.Options{
		Transports: []transport.Transport{polling.Default, websocket.Default, faultyTransport{}},
		RequestChecker: func(r *http.Request) (http.Header, error) {
			if r.Header.Get("X-Reject") != "" {
				return nil, errors.New("rejected")
			}
			return nil, nil
		},
	}
	rec := captureLogs(t, opts)
	srv := NewServer(opts)
	srv.OnConnect("/", func(Conn) error { return nil })
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	get := func(query, ctype string, header ...string) int {
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/?EIO=3&"+query, strings.NewReader("1:2"))
		require.NoError(t, err)
		if ctype != "" {
			req.Method = http.MethodPost
			req.Header.Set("Content-Type", ctype)
		}
		for i := 0; i < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, resp.Body)
		require.NoError(t, resp.Body.Close())
		return resp.StatusCode
	}
	dial := func(tr transport.Transport) string {
		u, err := url.Parse(ts.URL + "/?EIO=3")
		require.NoError(t, err)
		c, err := tr.Dial(u, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })
		if o, ok := c.(engineio.Opener); ok {
			params, err := o.Open()
			require.NoError(t, err)
			return params.SID
		}
		_, _, r, err := c.NextReader()
		require.NoError(t, err)
		params, err := transport.ReadConnParameters(r)
		require.NoError(t, err)
		return params.SID
	}
	wsSID, pollingSID := dial(websocket.Default), dial(polling.Default)
	require.Eventually(t, func() bool { return len(only(rec.since(0), "engineio: session open")) == 2 },
		waitFor, time.Millisecond, "a client can read OPEN before its session logs open")

	for _, tc := range []struct {
		reason, level string
		do            func() int
		status        int
	}{
		{"bad transport", "WARN", func() int { return get("transport=foo", "") }, http.StatusBadRequest},
		{"checker", "WARN", func() int { return get("transport=polling", "", "X-Reject", "1") }, http.StatusBadGateway},
		{"unknown sid", "DEBUG", func() int { return get("transport=polling&sid=nope", "") }, http.StatusBadRequest},
		{"accept", "WARN", func() int { return get("transport=websocket", "") }, http.StatusBadRequest},
		{"init", "WARN", func() int { return get("transport=faulty", "") }, http.StatusOK},
		{"bad upgrade", "WARN", func() int { return get("transport=polling&sid="+wsSID, "") }, http.StatusBadRequest},
		{"", "", func() int { return get("transport=polling&sid="+pollingSID, "application/bogus") }, http.StatusBadRequest},
	} {
		n := rec.len()
		require.Equal(t, tc.status, tc.do(), tc.reason)
		var rejected []map[string]string
		require.Eventually(t, func() bool {
			rejected = only(rec.since(n), "engineio: request rejected")
			return len(rejected) > 0 || tc.reason == ""
		}, waitFor, time.Millisecond, "no record for %q", tc.reason)
		time.Sleep(20 * time.Millisecond) // records the trigger might log late
		recs := rec.since(n)
		if tc.reason != "" {
			require.Len(t, only(recs, "engineio: request rejected"), 1, tc.reason)
			require.Equal(t, tc.reason, rejected[0]["reason"])
			require.Equal(t, tc.level, rejected[0]["level"], tc.reason)
		}
		require.Empty(t, loud(recs, "engineio: request rejected"), tc.reason)
		require.Empty(t, only(recs, "engineio: session open"), tc.reason)
		require.Empty(t, only(recs, "engineio: session close"), tc.reason)
		for _, m := range recs {
			require.NotContains(t, m["msg"], "superfluous", tc.reason)
		}
	}
}

// The lifecycle tests end while their last session still waits for its 500 ms ping
// timeout, so the session logs its close when a later test may already have installed a
// capture (CI run 37599816484 saw it in TestRequestRejectedRecords). Their servers must
// therefore keep their records off slog.Default. The record of the session below arrives
// about 500 ms after its handshake, long before the end of the wait.
func TestLifecycleServersKeepOffDefaultLogger(t *testing.T) {
	rec := newAttrRecorder()
	setDefault(t, rec)
	ts := newTestServer(t, func(srv *Server) {
		srv.OnConnect("/", func(Conn) error { return nil })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read(), "root namespace CONNECT from the server")
	require.Never(t, func() bool { return len(only(rec.since(0), "engineio: session close")) > 0 },
		1500*time.Millisecond, 10*time.Millisecond, "a session of a finished test logged to slog.Default")
}

// faultConn is a fakeConn whose first frame read fails after "2" with readErr, whose
// frame writers fail on Write with writeErr, and whose frames fail on Close with closeErr.
// With armed set, its writers fail only once armed is true.
type faultConn struct {
	*fakeConn
	readErr, writeErr, closeErr error
	armed                       *atomic.Bool
}

func (c faultConn) NextReader() (session.FrameType, io.ReadCloser, error) {
	if c.readErr == nil {
		ft, r, err := c.fakeConn.NextReader()
		return ft, struct {
			io.Reader
			io.Closer
		}{r, faultWriter{c}}, err
	}
	return session.TEXT, io.NopCloser(io.MultiReader(strings.NewReader("2"), iotest.ErrReader(c.readErr))), nil
}

func (c faultConn) NextWriter(ft session.FrameType) (io.WriteCloser, error) {
	if c.writeErr == nil && c.closeErr == nil || c.armed != nil && !c.armed.Load() {
		return c.fakeConn.NextWriter(ft)
	}
	return faultWriter{c}, nil
}

type faultWriter struct{ c faultConn }

func (w faultWriter) Write(p []byte) (int, error) {
	if w.c.writeErr != nil {
		return 0, w.c.writeErr
	}
	return len(p), nil
}

func (w faultWriter) Close() error { return w.c.closeErr }

// Covers 1L-T8 (S).
func TestUnhandledErrorRecords(t *testing.T) {
	failed, refused := errors.New("frame failed"), errors.New("refused")
	boom := map[string]interface{}{"boom": func(Conn) { panic("boom") }, "num": func(Conn, int) {}}
	armed := new(atomic.Bool)
	for _, tc := range []struct {
		name  string
		keep  bool // OnError stays registered
		fault faultConn
		h     hooks
		nsps  []string // of the expected "socketio: unhandled error" WARNs
		run   func(t *testing.T, p *peer)
	}{
		{name: "peer close reaches OnError as io.EOF", keep: true, run: func(t *testing.T, p *peer) {
			close(p.fc.peerGone)
			require.True(t, recv(t, p.errs, "the report").err == io.EOF)
		}},
		{name: "peer close", run: func(t *testing.T, p *peer) { close(p.fc.peerGone) }},
		{name: "mid-frame failure reaches OnError unchanged", keep: true, fault: faultConn{readErr: failed},
			run: func(t *testing.T, p *peer) { require.True(t, recv(t, p.errs, "the report").err == failed) }},
		{name: "handler panic with OnError", keep: true, h: hooks{events: boom}, run: func(t *testing.T, p *peer) {
			p.send(t, ev("boom"))
			recv(t, p.errs, "the report")
		}},
		{name: "handler panic", h: hooks{events: boom}, nsps: []string{"/"},
			run: func(t *testing.T, p *peer) { p.send(t, ev("boom")) }},
		{name: "connect without handlers", nsps: []string{"/nope"},
			run: func(t *testing.T, p *peer) { p.send(t, "0/nope") }},
		{name: "overflow", nsps: []string{"/a"}, run: func(t *testing.T, p *peer) {
			p.stall(t, p.join(t, "/a"), defaultWriteBufferSize+1)
		}},
		{name: "overflow in a failing OnConnect", nsps: []string{"/", "/"}, h: hooks{connect: func(c Conn) error {
			flood(c, defaultWriteBufferSize+1)
			return refused
		}}},
		{name: "argument decode error", h: hooks{events: boom}, nsps: []string{"/"},
			run: func(t *testing.T, p *peer) { p.send(t, `2["num","x"]`) }},
		{name: "unmarshalable argument", nsps: []string{"/a"},
			run: func(t *testing.T, p *peer) { p.join(t, "/a").Emit("bad", make(chan int)) }},
		{name: "NextWriter fails after connect", run: func(t *testing.T, p *peer) {
			p.fc.failWrite.Store(true)
			p.nc.Emit("x")
		}},
		{name: "frame Write fails", fault: faultConn{writeErr: failed}},
		{name: "frame Write fails after connect", fault: faultConn{writeErr: failed, armed: armed},
			run: func(t *testing.T, p *peer) { armed.Store(true); p.nc.Emit("x") }},
		{name: "frame Close fails", fault: faultConn{closeErr: failed}},
		{name: "frame Close fails, then a handler panic", fault: faultConn{closeErr: failed}, h: hooks{events: boom},
			nsps: []string{"/"}, run: func(t *testing.T, p *peer) { p.send(t, ev("boom")) }},
		{name: "frame Close fails, then OnConnect fails", fault: faultConn{closeErr: failed}, nsps: []string{"/"},
			h: hooks{connect: func(Conn) error { return refused }}},
		{name: "invalid packet type", nsps: []string{"/"}, run: func(t *testing.T, p *peer) { p.send(t, "9") }},
		{name: "empty frame", nsps: []string{"/"}, run: func(t *testing.T, p *peer) { p.send(t, "") }},
		{name: "EVENT without data", nsps: []string{"/"}, run: func(t *testing.T, p *peer) { p.send(t, "2") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := newAttrRecorder()
			setDefault(t, rec) // the peer's NewServer(nil) logs through logger.Log, which follows slog.Default
			p := newPeer(t, 'S', tc.h, "/a")
			for _, nsp := range []string{"/", "/a"} {
				if !tc.keep {
					p.srv.getNamespace(nsp).onError = nil
				}
			}
			tc.fault.fakeConn = p.fc
			p.srv.serveConn(tc.fault)
			if tc.run != nil {
				p.nc = recv(t, p.conns, "root OnConnect")
				tc.run(t, p)
				recv(t, p.conn().done, "the close") // except with closeErr, where nothing closes
			}
			var got []map[string]string
			require.Eventually(t, func() bool { got = loud(rec.since(0), ""); return len(got) >= len(tc.nsps) },
				waitFor, time.Millisecond)
			time.Sleep(20 * time.Millisecond) // a second record would come now
			got = loud(rec.since(0), "")
			var nsps []string
			for _, m := range got {
				require.Equal(t, "socketio: unhandled error", m["msg"])
				require.Equal(t, "WARN", m["level"])
				nsps = append(nsps, m["nsp"])
			}
			require.Equal(t, tc.nsps, nsps)
		})
	}
}

// Covers 1L-T10 (S).
func TestNamespaceRecords(t *testing.T) {
	refused := errors.New("refused")
	fail := func(nsp string, n int, err error) hooks { // OnConnect of nsp emits n packets and returns err
		return hooks{connect: func(c Conn) error {
			if c.Namespace() == nsp {
				flood(c, n)
				return err
			}
			return nil
		}}
	}
	for _, tc := range []struct {
		name        string
		h           hooks
		run         func(t *testing.T, p *peer)
		connects    map[string]error // the err of each namespace connect record
		disconnects map[string]any   // the reason of each disconnect record
	}{
		{"connect and disconnect", hooks{}, func(t *testing.T, p *peer) {
			p.connect(t).join(t, "/chat")
			p.join(t, "/b")
			p.send(t, `1/chat,["bye"]`)
			recv(t, p.discs, "OnDisconnect of /chat")
			close(p.fc.peerGone)
			p.disconnected(t, "/") // /b has no OnDisconnect
		}, map[string]error{"/": nil, "/chat": nil, "/b": nil},
			map[string]any{"/": "connection close", "/chat": "namespace disconnect", "/b": "connection close"}},
		{"root OnConnect fails", fail("/", 0, refused), func(t *testing.T, p *peer) {
			p.connect(t).disconnected(t, "/")
		}, map[string]error{"/": refused}, map[string]any{"/": "connection close"}},
		{"OnConnect of /chat fails", fail("/chat", 0, refused), func(t *testing.T, p *peer) {
			p.connect(t).send(t, "0/chat")
			p.disconnected(t, "/", "/chat")
		}, map[string]error{"/": nil, "/chat": refused},
			map[string]any{"/": "connection close", "/chat": "connection close"}},
		{"overflow in root OnConnect", fail("/", defaultWriteBufferSize+1, nil), func(t *testing.T, p *peer) {
			p.connect(t).disconnected(t, "/")
		}, map[string]error{"/": ErrWriteBufferFull}, map[string]any{"/": "connection close"}},
		{"overflow in a failing root OnConnect", fail("/", defaultWriteBufferSize+1, refused), func(t *testing.T, p *peer) {
			p.connect(t).disconnected(t, "/")
		}, map[string]error{"/": errors.Join(ErrWriteBufferFull, refused)}, map[string]any{"/": "connection close"}},
		{"overflow in OnConnect of /chat", fail("/chat", defaultWriteBufferSize+1, nil), func(t *testing.T, p *peer) {
			p.connect(t).stall(t, p.nc, 0) // so that the writer cannot drain the queue
			p.send(t, "0/chat")
			p.disconnected(t, "/", "/chat")
		}, map[string]error{"/": nil, "/chat": nil}, map[string]any{"/": "connection close", "/chat": "connection close"}},
		{"CONNECT after a draining Close", hooks{}, func(t *testing.T, p *peer) {
			p.connect(t).stall(t, p.nc, 0) // the drain waits for the writer
			require.NoError(t, p.Close())
			p.send(t, "0/chat")
			p.send(t, ev("x")) // returns once the CONNECT is handled
		}, map[string]error{"/": nil}, map[string]any{"/": "connection close"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingHandler{}
			setDefault(t, rec)
			p := newPeer(t, 'S', tc.h, "/chat", "/b")
			p.srv.getNamespace("/b").onDisconnect = nil
			p.srv.getNamespace("/chat").onError = nil
			tc.run(t, p)

			connects := rec.byNsp(t, "socketio: namespace connect", "err", slog.LevelDebug)
			require.Len(t, connects, len(tc.connects))
			for nsp, want := range tc.connects {
				err, _ := connects[nsp].(error)
				require.Equal(t, want == nil, err == nil, nsp)
				for _, w := range []error{refused, ErrWriteBufferFull} {
					require.Equal(t, errors.Is(want, w), errors.Is(err, w), "%s: %v", nsp, err)
				}
			}
			require.Equal(t, tc.disconnects, rec.byNsp(t, "socketio: disconnect", "reason", slog.LevelDebug))
			rec.mu.Lock()
			for _, r := range rec.recs {
				r.Attrs(func(a slog.Attr) bool { return assert.NotContains(t, a.Value.String(), "bye", "peer text") })
			}
			rec.mu.Unlock()
			if unhandled := func() map[string]any { // exactly one, at WARN
				return rec.byNsp(t, "socketio: unhandled error", "err", slog.LevelWarn)
			}; tc.name == "overflow in OnConnect of /chat" {
				require.Eventually(t, func() bool { return len(unhandled()) == 1 }, waitFor, time.Millisecond)
				time.Sleep(20 * time.Millisecond) // a second record would come now
				require.ErrorIs(t, unhandled()["/chat"].(error), ErrWriteBufferFull)
			}
		})
	}
}

// Covers 1L-T13 (P, W).
func TestPingTimeoutRecords(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			opts := &engineio.Options{PingTimeout: 100 * time.Millisecond}
			rec := captureLogs(t, opts)
			srv := NewServer(opts)
			connected := make(chan Conn, 1)
			srv.OnConnect("/", func(c Conn) error { connected <- c; return nil })
			go func() { _ = srv.Serve() }()
			t.Cleanup(func() { _ = srv.Close() })
			ts := httptest.NewServer(srv)
			t.Cleanup(ts.Close)

			if tr == polling.Default { // only the handshake request
				resp, err := http.Get(ts.URL + "/?EIO=3&transport=polling")
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
			} else {
				u, err := url.Parse(ts.URL + "/?EIO=3")
				require.NoError(t, err)
				c, err := tr.Dial(u, nil)
				require.NoError(t, err)
				t.Cleanup(func() { _ = c.Close() })
			}
			require.Eventually(t, func() bool {
				return len(only(rec.since(0), "engineio: session close")) == 1 && len(only(rec.since(0), "socketio: disconnect")) == 1
			}, waitFor, time.Millisecond)
			recs := rec.since(0)
			require.Empty(t, loud(recs, ""))
			require.Equal(t, "ping timeout", only(recs, "engineio: session close")[0]["reason"])
			require.Equal(t, "connection close", only(recs, "socketio: disconnect")[0]["reason"])
			_, hasErr := only(recs, "socketio: namespace connect")[0]["err"]
			require.Equal(t, tr == polling.Default, hasErr, "a namespace connect err")
			require.Equal(t, tr == polling.Default, len(connected) == 0, "root OnConnect calls")
		})
	}
}

// keyCheck collects the module's records that break the 1.L contract and, per message,
// the sid of each record ("" unless it has exactly one sid attribute).
type keyCheck struct {
	mu       sync.Mutex
	problems []string
	sids     map[string][]string
}

// keyChecker is the slog.Handler over a keyCheck; WithAttrs keeps the added attributes.
type keyChecker struct {
	*keyCheck
	attrs []slog.Attr
}

var (
	msgPattern  = regexp.MustCompile(`^(engineio|socketio|logger): [a-z][a-z0-9 ]*$`)
	allowedKeys = map[string]bool{"sid": true, "nsp": true, "err": true, "transport": true,
		"remote_addr": true, "reason": true, "duration": true, "event": true, "ack_id": true,
		"type": true, "value": true}
)

func newKeyChecker() *keyChecker {
	return &keyChecker{keyCheck: &keyCheck{sids: map[string][]string{}}}
}

func (h *keyChecker) Enabled(context.Context, slog.Level) bool { return true }

// Handle checks the records whose PC is in the module, except in the deprecated
// logger.Error and logger.Info. A !BADKEY attribute is a key outside the list.
func (h *keyChecker) Handle(_ context.Context, r slog.Record) error {
	f, _ := runtime.CallersFrames([]uintptr{r.PC}).Next()
	fn := strings.ReplaceAll(f.Function, "%2e", ".")
	const mod = "github.com/sshaplygin/go-socket.io"
	if !strings.HasPrefix(fn, mod) || fn == mod+"/logger.Error" || fn == mod+"/logger.Info" {
		return nil
	}
	attrs := append([]slog.Attr(nil), h.attrs...)
	r.Attrs(func(a slog.Attr) bool { attrs = append(attrs, a); return true })

	h.mu.Lock()
	defer h.mu.Unlock()
	bad := func(what string) { h.problems = append(h.problems, fmt.Sprintf("%s: %q from %s", what, r.Message, fn)) }
	if !msgPattern.MatchString(r.Message) {
		bad("message outside the pattern")
	}
	if r.Level != slog.LevelWarn && r.Level > slog.LevelDebug {
		bad("level " + r.Level.String())
	}
	var sids []string
	for _, a := range attrs {
		if !allowedKeys[a.Key] {
			bad("key " + a.Key)
		}
		if a.Key == "sid" {
			sids = append(sids, a.Value.String())
		}
	}
	sid := ""
	if len(sids) == 1 {
		sid = sids[0]
	}
	h.sids[r.Message] = append(h.sids[r.Message], sid)
	return nil
}

func (h *keyChecker) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &keyChecker{keyCheck: h.keyCheck, attrs: append(append([]slog.Attr(nil), h.attrs...), attrs...)}
}

func (h *keyChecker) WithGroup(string) slog.Handler { return h }

func (h *keyCheck) result(msg string) (problems, sids []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append(problems, h.problems...), append(sids, h.sids[msg]...)
}

// moduleGoroutines returns the IDs of the goroutines with a frame in a non-test file of
// the module.
func moduleGoroutines() map[string]bool {
	_, file, _, _ := runtime.Caller(0)
	dir := "\t" + file[:strings.LastIndex(file, "/")+1]
	buf := make([]byte, 1<<22)
	ids := map[string]bool{}
	for _, g := range strings.Split(string(buf[:runtime.Stack(buf, true)]), "\n\n") {
		for _, line := range strings.Split(g, "\n") {
			if f, _, _ := strings.Cut(line, ":"); strings.HasPrefix(f, dir) && !strings.HasSuffix(f, "_test.go") {
				ids[strings.Fields(g)[1]] = true
			}
		}
	}
	return ids
}

// TestNoBadKeyAttrs runs the TestLifecycleRootNamespace scenario at trace with checking
// handlers on Options.Logger and slog.Default.
//
// Covers 1L-T11 (S).
func TestNoBadKeyAttrs(t *testing.T) {
	inst, def := newKeyChecker(), newKeyChecker()
	setDefault(t, def)
	prev := logger.Level.Level()
	logger.Level.Set(logger.LevelTrace)
	t.Cleanup(func() { logger.Level.Set(prev) })

	before := moduleGoroutines()
	sid := lifecycleRootNamespace(t, &engineio.Options{Logger: slog.New(inst)})
	// Check the records only once the goroutines the scenario started, which include the
	// Go client's polling goroutines after Close, have all returned.
	require.Eventually(t, func() bool {
		for id := range moduleGoroutines() {
			if !before[id] {
				return false
			}
		}
		return true
	}, waitFor, 10*time.Millisecond)

	// The Go client's CONNECT to / follows the server's own root connect, so namespace
	// connect may appear more than once (1L-T10 owns the count). Each record carries
	// exactly one sid, the session's.
	for _, msg := range []string{"engineio: session open", "socketio: namespace connect", "socketio: disconnect"} {
		_, sids := inst.result(msg)
		assert.NotEmpty(t, sids, msg)
		for _, v := range sids {
			assert.Equal(t, sid, v, "sid of %q", msg)
		}
	}
	problems, _ := inst.result("")
	assert.Empty(t, problems, "instance logger")
	problems, _ = def.result("")
	assert.Empty(t, problems, "slog.Default")
}
