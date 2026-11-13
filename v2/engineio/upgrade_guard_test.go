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
	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
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

// badRequest and unknownSID are Engine.IO error bodies of HTTP 400 answers.
const badRequest, unknownSID = `{"code":3,"message":"Bad request"}`, `{"code":1,"message":"Session ID unknown"}`

// requireRefused checks that a websocket request was answered without the hijack, with
// HTTP 400 and the Engine.IO error body.
func requireRefused(t *testing.T, err error, body string) {
	t.Helper()
	var de websocket.DialError
	require.ErrorAs(t, err, &de)
	require.NotNil(t, de.Response, "no HTTP answer: %v", err)
	got, err := io.ReadAll(de.Response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, de.Response.StatusCode, "body %q", got)
	require.JSONEq(t, body, string(got))
}

// requireRejected checks that the server logged exactly one rejected request, for reason.
// It waits for the record: a handshake error reaches the client from the transport,
// before the server returns the reservation and logs.
func (f *guardFixture) requireRejected(t *testing.T, reason string) {
	t.Helper()
	require.Eventually(t, func() bool { return len(f.rec.Find("engineio: request rejected")) > 0 },
		5*time.Second, time.Millisecond)
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
			requireRefused(t, err, badRequest)
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

// namedWebsocket is a transport of another type registered under the name "websocket".
type namedWebsocket struct{ *websocket.Transport }

// TestUpgradeGuardBusySession: a websocket request with the sid of a session already on
// websocket, upgraded or opened there, is answered before the hijack with HTTP 400, code
// 3 and one "bad upgrade" record, also when the transport named "websocket" is not a
// *websocket.Transport. An opening websocket request, without sid, succeeds.
func TestUpgradeGuardBusySession(t *testing.T) {
	const other = "opened on another transport named websocket"
	for _, name := range []string{"upgraded", "opened on websocket", other} {
		t.Run(name, func(t *testing.T) {
			opts := &engineio.Options{}
			if name == other {
				opts.Transports = []transport.Transport{polling.Default, namedWebsocket{websocket.Default}}
			}
			f := newGuardFixture(t, opts)
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
			requireRefused(t, err, badRequest)
			f.requireRejected(t, "bad upgrade")
			require.Equal(t, "websocket", conn.(*session.Session).Transport())
		})
	}
}

// TestUpgradeGuardClosedSession: a request the session would not serve, with the sid of a
// session that is closed but still registered, gets the answer of an unknown sid, as on
// the Node server, which deletes a closed client: HTTP 400, code 1 and an "unknown sid"
// record. Here a websocket request for a session closed on polling, then a polling
// request for a session closed on websocket.
func TestUpgradeGuardClosedSession(t *testing.T) {
	f := newGuardFixture(t, &engineio.Options{})
	dialRaw(t, polling.Default, f.url)
	conn := f.accept(t)
	require.NoError(t, conn.Close())
	require.Equal(t, 1, f.srv.Count(), "the closed session is still registered")

	_, err := f.dial(t, conn.ID(), nil)
	requireRefused(t, err, unknownSID)
	f.requireRejected(t, "unknown sid")

	_, err = f.dial(t, "", nil)
	require.NoError(t, err)
	conn = f.accept(t)
	require.NoError(t, conn.Close())
	w := httptest.NewRecorder()
	f.srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/?EIO=4&transport=polling&sid="+conn.ID(), nil))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(t, unknownSID, w.Body.String())
	got := f.rec.Find("engineio: request rejected")
	require.Len(t, got, 2)
	require.Equal(t, "unknown sid", got[1]["reason"])
}

// TestUpgradeGuardRejectedAccept: an upgrade request the transport refuses to accept,
// for its origin, as a malformed handshake or for a writer without http.Hijacker,
// returns the reservation, so the next upgrade of the session completes.
func TestUpgradeGuardRejectedAccept(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		status       int
		send         func(t *testing.T, f *guardFixture, sid string) int
	}{
		{"origin", "accept", http.StatusForbidden, func(t *testing.T, f *guardFixture, sid string) int {
			_, err := f.dial(t, sid, http.Header{"Origin": {"http://elsewhere.example"}})
			var de websocket.DialError
			require.ErrorAs(t, err, &de)
			return de.Response.StatusCode
		}},
		{"handshake", "accept", http.StatusBadRequest, func(t *testing.T, f *guardFixture, sid string) int {
			resp, err := http.Get(f.url + "/?EIO=4&transport=websocket&sid=" + sid) // no upgrade headers
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			return resp.StatusCode
		}},
		{"no hijacker", "no hijacker", http.StatusNotImplemented, func(t *testing.T, f *guardFixture, sid string) int {
			w := httptest.NewRecorder()
			f.srv.ServeHTTP(plainWriter{w}, httptest.NewRequest(http.MethodGet, "/?EIO=4&transport=websocket&sid="+sid, nil))
			return w.Code
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGuardFixture(t, &engineio.Options{})
			dialRaw(t, polling.Default, f.url)
			conn := f.accept(t)
			require.Equal(t, tc.status, tc.send(t, f, conn.ID()))
			f.requireRejected(t, tc.reason)

			ws, err := f.dial(t, conn.ID(), nil)
			require.NoError(t, err)
			upgradeTo(t, ws, conn)
		})
	}
}

// TestUpgradeTimeout: an upgrade that never sends the probe, and one whose probe is
// answered but never followed by UPGRADE, are closed at UpgradeTimeout from the accept of
// their websocket connection. The session stays on polling, unpaused: it serves the next
// poll, and a later upgrade completes and outlives the upgrade window.
func TestUpgradeTimeout(t *testing.T) {
	const timeout = time.Second
	f := newGuardFixture(t, &engineio.Options{UpgradeTimeout: timeout})
	p := dialRaw(t, polling.Default, f.url)
	conn := f.accept(t)

	// Each attempt is closed at the timeout from its accept: one that sends no probe, then
	// one whose probe comes late, at 70% of the window, is answered and is not followed by
	// UPGRADE. The close must come before 135% of the window: 35% for scheduling under
	// -race, and 35% short of a window measured from the late probe (170%).
	for _, probeAt := range []time.Duration{-1, timeout * 7 / 10} {
		start := time.Now()
		ws, err := f.dial(t, conn.ID(), nil)
		require.NoError(t, err)
		if probeAt >= 0 {
			time.Sleep(time.Until(start.Add(probeAt)))
			sendProbe(t, ws)
		}
		_, err = nextType(ws, 5*time.Second)
		elapsed := time.Since(start)
		require.Error(t, err, "the server closes the websocket connection")
		require.GreaterOrEqual(t, elapsed, timeout)
		require.Less(t, elapsed, timeout*135/100)
	}

	// A failed upgrade returns the reservation before the server closes the connection, so
	// a retry right after the close is accepted, however late the rest of the failed
	// upgrade ends: here after the expired window, then after a packet other than the
	// probe, which fails the upgrade at once.
	ws, err := f.dial(t, conn.ID(), nil)
	require.NoError(t, err)
	writePacket(t, ws, packet.MESSAGE, "not a probe")
	_, err = nextType(ws, 5*time.Second)
	require.Error(t, err, "the server closes the websocket connection")
	ws, err = f.dial(t, conn.ID(), nil)
	require.NoError(t, err)

	writeLater(conn, "after the timeout")
	pt, data := readPacket(t, p)
	require.Equal(t, packet.MESSAGE, pt)
	require.Equal(t, "after the timeout", data)
	require.Equal(t, "polling", conn.(*session.Session).Transport())
	upgradeTo(t, ws, conn)

	// The switch cancels the window timer and gives the connection the heartbeat's
	// deadline: once the window has passed, the session still writes there. The wait is
	// twice the window, so that a timer or deadline left on the connection would have
	// fired before the write.
	time.Sleep(2 * timeout)
	writeLater(conn, "after the switch")
	pt, data = readPacket(t, ws)
	require.Equal(t, packet.MESSAGE, pt)
	require.Equal(t, "after the switch", data)
}

// writeLater writes a MESSAGE with data to conn from another goroutine.
func writeLater(conn engineio.Conn, data string) {
	go func() {
		if w, err := conn.NextWriter(frame.String); err == nil {
			_, _ = w.Write([]byte(data))
			_ = w.Close()
		}
	}()
}

// TestUpgradeTimeoutNegative: Normalize rejects a negative UpgradeTimeout, and NewServer
// treats it as zero, 10 s: the upgrade completes, where a window that is already over
// would close the connection before the probe.
func TestUpgradeTimeoutNegative(t *testing.T) {
	_, err := engineio.Options{UpgradeTimeout: -time.Second}.Normalize()
	require.Error(t, err)
	valid := engineio.Options{UpgradeTimeout: time.Second}
	got, err := valid.Normalize()
	require.NoError(t, err)
	require.Equal(t, valid, got)

	f := newGuardFixture(t, &engineio.Options{UpgradeTimeout: -time.Second})
	dialRaw(t, polling.Default, f.url)
	conn := f.accept(t)
	ws, err := f.dial(t, conn.ID(), nil)
	require.NoError(t, err)
	upgradeTo(t, ws, conn)
}
