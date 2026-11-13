package engineio_test

import (
	"io"
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
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio"
	"github.com/sshaplygin/go-socket.io/v2/engineio/internal/logtest"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
	"github.com/sshaplygin/go-socket.io/v2/engineio/session"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/websocket"
)

// guardFixture is an engineio.Server with a recording logger behind an HTTP server whose
// listener tracks the connections it accepts. Its cleanup runs after the test's own: it
// closes the server, then requires that every accepted connection, hijacked ones
// included, has been closed and that no goroutine started during the test still runs
// code of an engineio package.
type guardFixture struct {
	srv *engineio.Server
	url string
	rec *logtest.Recorder
}

type trackingListener struct {
	net.Listener
	mu    sync.Mutex
	conns []*trackedConn
}

type trackedConn struct {
	net.Conn
	closed atomic.Bool
}

func (c *trackedConn) Close() error { c.closed.Store(true); return c.Conn.Close() }

func (l *trackingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	tc := &trackedConn{Conn: c}
	l.mu.Lock()
	l.conns = append(l.conns, tc)
	l.mu.Unlock()
	return tc, nil
}

func (l *trackingListener) open() (n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range l.conns {
		if !c.closed.Load() {
			n++
		}
	}
	return n
}

// engineCode matches a stack frame of an engineio package, not of this test package.
var engineCode = regexp.MustCompile(`go-socket\.io/v2/engineio[./]`)

// engineGoroutines returns the stacks that run engineio code, keyed by goroutine,
// except the goroutines of skip.
func engineGoroutines(skip map[string]string) map[string]string {
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, true)
	for ; n == len(buf); n = runtime.Stack(buf, true) {
		buf = make([]byte, 2*len(buf))
	}
	gs := map[string]string{}
	for _, g := range strings.Split(string(buf[:n]), "\n\n") {
		id, _, _ := strings.Cut(g, " [")
		if _, ok := skip[id]; !ok && engineCode.MatchString(g) {
			gs[id] = g
		}
	}
	return gs
}

func newGuardFixture(t *testing.T, opts *engineio.Options) *guardFixture {
	before := engineGoroutines(nil)
	f := &guardFixture{rec: logtest.NewRecorder()}
	opts.Logger = slog.New(f.rec)
	f.srv = engineio.NewServer(opts)
	ts := httptest.NewUnstartedServer(f.srv)
	ln := &trackingListener{Listener: ts.Listener}
	ts.Listener = ln
	ts.Start()
	f.url = ts.URL
	t.Cleanup(func() {
		for start := time.Now(); ; time.Sleep(5 * time.Millisecond) {
			open, left := ln.open(), engineGoroutines(before)
			if open == 0 && len(left) == 0 {
				return
			}
			if time.Since(start) > 5*time.Second {
				gs := make([]string, 0, len(left))
				for _, g := range left {
					gs = append(gs, g)
				}
				t.Fatalf("connections open: %d; goroutines left:\n%s", open, strings.Join(gs, "\n\n"))
			}
		}
	})
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = f.srv.Close() })
	return f
}

// accept returns the next session of the server; the test's cleanup closes it.
func (f *guardFixture) accept(t *testing.T) engineio.Conn {
	conn, err := f.srv.Accept()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// dial sends a websocket request for the session sid, or an opening one when sid is
// empty. It may run outside the test goroutine.
func (f *guardFixture) dial(t *testing.T, sid string, header http.Header) (transport.Conn, error) {
	u, err := url.Parse(f.url + "/?EIO=4&sid=" + sid)
	if err != nil {
		return nil, err
	}
	if sid == "" {
		u.RawQuery = "EIO=4"
	}
	ws, err := websocket.Default.Dial(u, header)
	if err == nil {
		t.Cleanup(func() { _ = ws.Close() })
	}
	return ws, err
}

// requireBadUpgrade checks that a websocket request was answered without the hijack,
// with HTTP 400 and the Engine.IO error code 3.
func requireBadUpgrade(t *testing.T, err error) {
	t.Helper()
	var de websocket.DialError
	require.ErrorAs(t, err, &de)
	require.NotNil(t, de.Response, "no HTTP answer: %v", err)
	body, err := io.ReadAll(de.Response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, de.Response.StatusCode, "body %q", body)
	require.JSONEq(t, `{"code":3,"message":"Bad request"}`, string(body))
}

// requireRejected checks that the server logged exactly one rejected request, for reason.
func (f *guardFixture) requireRejected(t *testing.T, reason string) {
	t.Helper()
	got := f.rec.Find("engineio: request rejected")
	require.Len(t, got, 1)
	require.Equal(t, reason, got[0]["reason"])
}

// sendProbe sends the upgrade probe on ws and reads its answer.
func sendProbe(t *testing.T, ws transport.Conn) {
	t.Helper()
	writePacket(t, ws, packet.PING, "probe")
	pt, data := readPacket(t, ws)
	require.Equal(t, packet.PONG, pt)
	require.Equal(t, "probe", data)
}

// upgradeTo completes the upgrade of the session of conn on ws.
func upgradeTo(t *testing.T, ws transport.Conn, conn engineio.Conn) {
	t.Helper()
	sendProbe(t, ws)
	writePacket(t, ws, packet.UPGRADE, "")
	require.Eventually(t, func() bool { return conn.(*session.Session).Transport() == "websocket" },
		5*time.Second, time.Millisecond)
}

// TestUpgradeGuardConcurrentUpgrades: two websocket requests race to upgrade one polling
// session, and each accepted one is driven as a client would, with a probe and UPGRADE.
// Exactly one is accepted and switches the session; the other is answered before the
// hijack with HTTP 400, code 3 and one "bad upgrade" record. The fixture's cleanup finds
// every connection closed, so no switched-out connection is left behind.
func TestUpgradeGuardConcurrentUpgrades(t *testing.T) {
	f := newGuardFixture(t, &engineio.Options{})
	dialRaw(t, polling.Default, f.url)
	conn := f.accept(t)

	dials := make(chan error, 2)
	var mu sync.Mutex
	var accepted []transport.Conn
	for i := 0; i < 2; i++ {
		go func() {
			ws, err := f.dial(t, conn.ID(), nil)
			if err == nil {
				mu.Lock()
				accepted = append(accepted, ws)
				mu.Unlock()
			}
			dials <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-dials; err != nil {
			requireBadUpgrade(t, err)
		}
	}
	for _, ws := range accepted {
		sendProbe(t, ws)
	}
	for _, ws := range accepted {
		writePacket(t, ws, packet.UPGRADE, "")
	}
	require.Eventually(t, func() bool { return conn.(*session.Session).Transport() == "websocket" },
		5*time.Second, time.Millisecond)
	require.Len(t, accepted, 1, "one request upgrades the session, the other is rejected")
	f.requireRejected(t, "bad upgrade")
}

// TestUpgradeGuardBusySession: a websocket request with the sid of a session already on
// websocket, upgraded or opened there, is answered before the hijack with HTTP 400, code
// 3 and one "bad upgrade" record. An opening websocket request, without sid, succeeds.
func TestUpgradeGuardBusySession(t *testing.T) {
	for _, name := range []string{"upgraded", "opened on websocket"} {
		t.Run(name, func(t *testing.T) {
			f := newGuardFixture(t, &engineio.Options{})
			var conn engineio.Conn
			if name == "upgraded" {
				dialRaw(t, polling.Default, f.url)
				conn = f.accept(t)
				ws, err := f.dial(t, conn.ID(), nil)
				require.NoError(t, err)
				upgradeTo(t, ws, conn)
			} else {
				_, err := f.dial(t, "", nil)
				require.NoError(t, err, "an opening websocket request succeeds")
				conn = f.accept(t)
			}

			_, err := f.dial(t, conn.ID(), nil)
			requireBadUpgrade(t, err)
			f.requireRejected(t, "bad upgrade")
			require.Equal(t, "websocket", conn.(*session.Session).Transport())
		})
	}
}
