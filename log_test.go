package socketio

import (
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

// byNsp maps the nsp of each record msg to the value of its attribute key (nil if absent).
func (h *recordingHandler) byNsp(msg, key string) map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]any{}
	for _, r := range h.recs {
		m := map[string]slog.Value{}
		r.Attrs(func(a slog.Attr) bool { m[a.Key] = a.Value; return true })
		if r.Message == msg {
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

// faultConn is a fakeConn whose first frame read fails after "2" with readErr, and whose
// frame writers fail on Write with writeErr or on Close with closeErr.
type faultConn struct {
	*fakeConn
	readErr, writeErr, closeErr error
}

func (c faultConn) NextReader() (session.FrameType, io.ReadCloser, error) {
	if c.readErr == nil {
		return c.fakeConn.NextReader()
	}
	return session.TEXT, io.NopCloser(io.MultiReader(strings.NewReader("2"), iotest.ErrReader(c.readErr))), nil
}

func (c faultConn) NextWriter(ft session.FrameType) (io.WriteCloser, error) {
	if c.writeErr == nil && c.closeErr == nil {
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
		{name: "frame Write fails", fault: faultConn{writeErr: failed}},
		{name: "frame Close fails", fault: faultConn{closeErr: failed}},
		{name: "invalid packet type", nsps: []string{"/"}, run: func(t *testing.T, p *peer) { p.send(t, "9") }},
		{name: "empty frame", nsps: []string{"/"}, run: func(t *testing.T, p *peer) { p.send(t, "") }},
		{name: "EVENT without data", nsps: []string{"/"}, run: func(t *testing.T, p *peer) { p.send(t, "2") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := captureLogs(t, &engineio.Options{})
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

			connects := rec.byNsp("socketio: namespace connect", "err")
			require.Len(t, connects, len(tc.connects))
			for nsp, want := range tc.connects {
				err, _ := connects[nsp].(error)
				require.Equal(t, want == nil, err == nil, nsp)
				for _, w := range []error{refused, ErrWriteBufferFull} {
					require.Equal(t, errors.Is(want, w), errors.Is(err, w), "%s: %v", nsp, err)
				}
			}
			require.Equal(t, tc.disconnects, rec.byNsp("socketio: disconnect", "reason"))
			rec.mu.Lock()
			for _, r := range rec.recs {
				r.Attrs(func(a slog.Attr) bool { return assert.NotContains(t, a.Value.String(), "bye", "peer text") })
			}
			rec.mu.Unlock()
			if tc.name == "overflow in OnConnect of /chat" {
				require.Eventually(t, func() bool { return len(rec.byNsp("socketio: unhandled error", "err")) == 1 },
					waitFor, time.Millisecond)
				require.Contains(t, rec.byNsp("socketio: unhandled error", "err"), "/chat")
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
