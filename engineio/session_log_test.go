package engineio

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// recorder keeps each record as its message, level and attributes, including
// the attributes added through WithAttrs.
type recorder struct {
	mu    *sync.Mutex
	recs  *[]map[string]string
	attrs []slog.Attr
}

func newRecorder() *recorder { return &recorder{mu: new(sync.Mutex), recs: new([]map[string]string)} }

func (h *recorder) Enabled(context.Context, slog.Level) bool { return true }

// bridged matches a message that the log package carried from the previous default handler into
// slog.Default (slog.SetDefault redirects log's output there at INFO): a goroutine that outlives
// an earlier test writes its line in the old handler's own format, never a library message.
var bridged = regexp.MustCompile(`^(\d{4}/\d\d/\d\d \d\d:\d\d:\d\d(\.\d+)? )?(DEBUG|INFO|WARN|ERROR)([+-]\d+)? `)

func (h *recorder) Handle(_ context.Context, r slog.Record) error {
	if bridged.MatchString(r.Message) {
		return nil
	}
	m := map[string]string{"msg": r.Message, "level": r.Level.String()}
	add := func(a slog.Attr) bool { m[a.Key] = a.Value.String(); return true }
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(add)
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.recs = append(*h.recs, m)
	return nil
}

func (h *recorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *recorder) WithGroup(string) slog.Handler { return h }

// setDefault makes h the default handler until Cleanup, which also restores the log
// package's output and flags that slog.SetDefault changes; not for parallel tests.
func setDefault(t *testing.T, h slog.Handler) {
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev); log.SetOutput(prevOut); log.SetFlags(prevFlags) })
}

// find returns the records of msg, or all records if msg is empty.
func (h *recorder) find(msg string) (out []map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range *h.recs {
		if msg == "" || m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

// logFixture is an engineio.Server with a recording logger and a client
// dialled at the transport level, so that it sends no pings.
type logFixture struct {
	srv *Server
	url string
	cl  transport.Conn
}

func (f *logFixture) accept(t *testing.T) Conn {
	conn, err := f.srv.Accept()
	require.NoError(t, err)
	return conn
}

func (f *logFixture) send(t *testing.T, pt packet.Type, data string) {
	w, err := f.cl.NextWriter(frame.String, pt)
	require.NoError(t, err)
	_, err = w.Write([]byte(data))
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

// readAll reads conn until NextReader fails; the session then has closed itself.
func readAll(conn Conn) error {
	for {
		_, r, err := conn.NextReader()
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, r)
		_ = r.Close()
	}
}

// upgrade moves the session of conn to websocket with a probe and an UPGRADE packet.
func (f *logFixture) upgrade(t *testing.T, conn Conn) {
	u, err := url.Parse(f.url + "/?EIO=3&sid=" + conn.ID())
	require.NoError(t, err)
	f.cl, err = websocket.Default.Dial(u, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.cl.Close() })
	f.send(t, packet.PING, "probe")
	_, pt, r, err := f.cl.NextReader()
	require.NoError(t, err)
	require.Equal(t, packet.PONG, pt)
	require.NoError(t, r.Close())
	f.send(t, packet.UPGRADE, "")
	require.Eventually(t, func() bool { return conn.(*session.Session).Transport() == "websocket" },
		time.Second, time.Millisecond)
}

// Covers 1L-T1 (P, W).
// Covers 1L-T2 (W).
// Covers 1L-T3 (P, W).
// Covers 1L-T4 (P, W).
// Covers 1L-T5 (P).
// Covers 1L-T6 (P, W).
// Covers 1L-T7 (P, W).
func TestSessionCloseRecord(t *testing.T) {
	cases := []struct {
		name, sides, reason, transport string // transport, if set, is the one at close
		run                            func(t *testing.T, f *logFixture)
	}{
		{"close packet", "PW", "transport close", "", func(t *testing.T, f *logFixture) {
			conn := f.accept(t)
			f.send(t, packet.CLOSE, "")
			require.Equal(t, io.EOF, readAll(conn))
		}},
		{"close packet then Close", "PW", "transport close", "", func(t *testing.T, f *logFixture) {
			conn := f.accept(t)
			f.send(t, packet.CLOSE, "")
			require.Equal(t, io.EOF, readAll(conn))
			require.NoError(t, conn.Close())
		}},
		{"peer close", "W", "transport error", "", func(t *testing.T, f *logFixture) {
			conn := f.accept(t)
			require.NoError(t, f.cl.Close())
			require.Error(t, readAll(conn))
		}},
		{"ping timeout", "PW", "ping timeout", "", func(t *testing.T, f *logFixture) {
			require.Error(t, readAll(f.accept(t)))
		}},
		{"ping timeout after upgrade", "P", "ping timeout", "websocket", func(t *testing.T, f *logFixture) {
			conn := f.accept(t)
			f.upgrade(t, conn)
			require.Error(t, readAll(conn))
		}},
		{"peer close after upgrade", "P", "transport error", "websocket", func(t *testing.T, f *logFixture) {
			start, conn := time.Now(), f.accept(t)
			time.Sleep(200 * time.Millisecond) // of PingTimeout 400 ms
			f.upgrade(t, conn)
			time.Sleep(time.Until(start.Add(480 * time.Millisecond))) // past the handshake's deadline
			require.NoError(t, f.cl.Close())
			require.Error(t, readAll(conn))
		}},
		{"Close after a message", "PW", "forced close", "", func(t *testing.T, f *logFixture) {
			conn := f.accept(t)
			f.send(t, packet.MESSAGE, "hi")
			_, r, err := conn.NextReader()
			require.NoError(t, err)
			b, err := io.ReadAll(r)
			require.NoError(t, err)
			require.Equal(t, "hi", string(b))
			require.NoError(t, r.Close())
			require.NoError(t, conn.Close())
		}},
		{"server close before Accept", "P", "server shutting down", "", func(t *testing.T, f *logFixture) {
			require.Eventually(t, func() bool { return len(f.srv.connChan) == 1 }, time.Second, time.Millisecond)
			require.NoError(t, f.srv.Close())
		}},
	}
	for _, tc := range cases {
		for _, side := range tc.sides {
			tr := map[rune]string{'P': "polling", 'W': "websocket"}[side]
			t.Run(tc.name+"/"+tr, func(t *testing.T) {
				rec := newRecorder()
				opts := &Options{Logger: slog.New(rec)}
				opts.PingTimeout = map[string]time.Duration{"ping timeout": 100 * time.Millisecond, "transport error": 400 * time.Millisecond}[tc.reason]
				f := &logFixture{srv: NewServer(opts)}
				ts := httptest.NewServer(f.srv)
				t.Cleanup(ts.Close)
				t.Cleanup(func() { _ = f.srv.Close() })
				f.url = ts.URL

				u, err := url.Parse(ts.URL + "/?EIO=3")
				require.NoError(t, err)
				if tr == "polling" {
					f.cl, err = polling.Default.Dial(u, nil)
					require.NoError(t, err)
					_, err = f.cl.(Opener).Open()
				} else {
					f.cl, err = websocket.Default.Dial(u, nil)
				}
				require.NoError(t, err)
				cl := f.cl
				t.Cleanup(func() { _ = cl.Close() })

				tc.run(t, f)
				closes := rec.find("engineio: session close")
				require.Len(t, closes, 1)
				m := closes[0]
				require.Equal(t, tc.reason, m["reason"])
				if tc.transport != "" {
					tr = tc.transport
				}
				require.Equal(t, tr, m["transport"])
				d, err := time.ParseDuration(m["duration"])
				require.NoError(t, err, "duration %q", m["duration"])
				if tc.reason == "ping timeout" {
					require.Positive(t, d)
				}
				_, hasErr := m["err"]
				require.Equal(t, tc.reason == "transport error", hasErr, "err %q", m["err"])
			})
		}
	}
}

// TestDialFailureRecords dials through two failing polling transports and an invalid URL
// with slog.Default recording. The failure Dial does not return is one WARN; the errors
// it returns and the polling client's request failures, which reach its reader, are
// DEBUG; every message follows the 1.L pattern. Not parallel: it sets slog.Default.
func TestDialFailureRecords(t *testing.T) {
	rec := newRecorder()
	setDefault(t, rec)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer ts.Close()
	d := Dialer{Transports: []transport.Transport{polling.Default, polling.Default}}
	_, err := d.Dial(ts.URL, nil)
	require.Error(t, err)
	_, err = d.Dial("http://[::1", nil)
	require.Error(t, err)

	pattern := regexp.MustCompile(`^(engineio|socketio|logger): [a-z][a-z0-9 ]*$`)
	var loud []map[string]string
	for _, m := range rec.find("") {
		assert.Regexp(t, pattern, m["msg"])
		if m["level"] != slog.LevelDebug.String() {
			loud = append(loud, m)
		}
	}
	require.Len(t, loud, 1)
	assert.Equal(t, "engineio: transport dial failed", loud[0]["msg"])
	assert.Equal(t, slog.LevelWarn.String(), loud[0]["level"])
	assert.Equal(t, "polling", loud[0]["transport"])
	assert.Len(t, rec.find("engineio: transport dial failed"), 2)
	assert.Len(t, rec.find("engineio: parse url failed"), 1)
}

// TestRecorderDropsBridgedLines checks that a line the log package carries from the old
// default handler is not a record of the test, while a library record is.
func TestRecorderDropsBridgedLines(t *testing.T) {
	rec := newRecorder()
	for _, msg := range []string{
		`2026/10/09 22:50:11 WARN engineio: request rejected transport=polling reason=init err=EOF`,
		`2026/10/09 21:22:17 DEBUG engineio: get request failed err="refused"`,
		`engineio: request rejected`,
	} {
		require.NoError(t, rec.Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelInfo, msg, 0)))
	}
	require.Len(t, rec.find(""), 1)
	require.Len(t, rec.find("engineio: request rejected"), 1)
}

// TestClientPeerCloseRecords closes the accepted session of an engineio.Dialer client
// that does not close itself, or has a websocket peer send a close frame after OPEN,
// reads the client until NextReader fails and waits for its ping to fail, with
// slog.Default recording: the 1.L Levels rule makes a ping failure after a peer close
// expected closure, so no record is above DEBUG. Not parallel.
//
// Covers the 1.L Levels rule on the engine.io client (P, W); no 1L-T case.
func TestClientPeerCloseRecords(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			rec := newRecorder()
			setDefault(t, rec)
			srv := NewServer(&Options{PingInterval: 50 * time.Millisecond, Transports: []transport.Transport{tr}})
			defer func() { _ = srv.Close() }()
			ts := httptest.NewServer(srv)
			defer ts.Close()

			cl, err := (&Dialer{Transports: []transport.Transport{tr}}).Dial(ts.URL, nil)
			require.NoError(t, err)
			defer func() { _ = cl.Close() }()
			conn, err := srv.Accept()
			require.NoError(t, err)
			require.NoError(t, conn.Close())
			require.Error(t, readAll(cl))
			require.Eventually(t, func() bool { return len(rec.find("engineio: ping failed")) > 0 },
				5*time.Second, time.Millisecond)
			for _, m := range rec.find("") {
				assert.Equal(t, slog.LevelDebug.String(), m["level"], "%v", m)
			}
		})
	}
	t.Run("websocket close frame", func(t *testing.T) {
		rec := newRecorder()
		setDefault(t, rec)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = c.Close() }()
			open := `0{"sid":"x","upgrades":[],"pingInterval":50,"pingTimeout":5000}`
			_ = c.WriteMessage(gorilla.TextMessage, []byte(open))
			msg := gorilla.FormatCloseMessage(gorilla.CloseNormalClosure, "")
			_ = c.WriteControl(gorilla.CloseMessage, msg, time.Now().Add(time.Second))
			for err == nil {
				_, _, err = c.NextReader()
			}
		}))
		defer ts.Close()

		cl, err := (&Dialer{Transports: []transport.Transport{websocket.Default}}).Dial(ts.URL, nil)
		require.NoError(t, err)
		defer func() { _ = cl.Close() }()
		require.Error(t, readAll(cl))
		require.Eventually(t, func() bool { return len(rec.find("engineio: ping failed")) > 0 },
			5*time.Second, time.Millisecond)
		for _, m := range rec.find("") {
			assert.Equal(t, slog.LevelDebug.String(), m["level"], "%v", m)
		}
	})
}

// brokenConn is a transport.Conn whose first reader is a PONG that fails to close with
// errBroken, the failure its next NextReader returns, as transports keep read failures.
type brokenConn struct {
	transport.Conn
	reads int
}

var errBroken = errors.New("broken")

type brokenReader struct{ io.Reader }

func (brokenReader) Close() error { return errBroken }

func (c *brokenConn) NextReader() (frame.Type, packet.Type, io.ReadCloser, error) {
	if c.reads++; c.reads == 1 {
		return frame.String, packet.PONG, brokenReader{strings.NewReader("")}, nil
	}
	return 0, 0, nil, errBroken
}

func (c *brokenConn) SetReadDeadline(time.Time) error { return nil }

func (c *brokenConn) NextWriter(frame.Type, packet.Type) (io.WriteCloser, error) {
	return nil, errBroken
}

func (c *brokenConn) Close() error { return nil }

// TestClientPingFailureRecord: a ping failure that is not expected closure (NextWriter
// fails with errBroken while the client is open) is one WARN record, since no caller
// receives it. Not parallel: it sets slog.Default.
//
// Covers the 1.L Levels rule on the engine.io client; no 1L-T case.
func TestClientPingFailureRecord(t *testing.T) {
	rec := newRecorder()
	setDefault(t, rec)
	params := transport.ConnParameters{PingInterval: time.Millisecond}
	(&client{conn: &brokenConn{}, params: params, close: make(chan struct{})}).serve()
	recs := rec.find("engineio: ping failed")
	require.Len(t, recs, 1)
	assert.Equal(t, slog.LevelWarn.String(), recs[0]["level"])
	assert.Equal(t, errBroken.Error(), recs[0]["err"])
}

// TestClientReaderCloseRecord: a failing reader Close in the engine.io client's
// NextReader, whose failure NextReader then returns, logs nothing above DEBUG under the
// 1.L Levels rule. Not parallel: it sets slog.Default.
//
// Covers the 1.L Levels rule on the engine.io client; no 1L-T case.
func TestClientReaderCloseRecord(t *testing.T) {
	rec := newRecorder()
	setDefault(t, rec)
	_, _, err := (&client{conn: &brokenConn{}, close: make(chan struct{})}).NextReader()
	require.ErrorIs(t, err, errBroken)
	require.Len(t, rec.find("engineio: close reader failed"), 1)
	for _, m := range rec.find("") {
		assert.Equal(t, slog.LevelDebug.String(), m["level"], "%v", m)
	}
}
