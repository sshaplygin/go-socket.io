package engineio

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	sessionImpl "github.com/sshaplygin/go-socket.io/engineio/internal/session"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
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

func (f *logFixture) send(t *testing.T, pt eioparser.PacketType, data string) {
	w, err := f.cl.NextWriter(eioparser.String, pt)
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
	f.send(t, eioparser.PING, "probe")
	_, pt, r, err := f.cl.NextReader()
	require.NoError(t, err)
	require.Equal(t, eioparser.PONG, pt)
	require.NoError(t, r.Close())
	f.send(t, eioparser.UPGRADE, "")
	require.Eventually(t, func() bool { return conn.(*sessionImpl.Session).Transport() == "websocket" },
		time.Second, time.Millisecond)
}

func TestSessionCloseRecord(t *testing.T) {
	cases := []struct {
		name, sides, reason, transport string // transport, if set, is the one at close
		run                            func(t *testing.T, f *logFixture)
	}{
		{"close packet", "PW", "transport close", "", func(t *testing.T, f *logFixture) {
			conn := f.accept(t)
			f.send(t, eioparser.CLOSE, "")
			require.ErrorIs(t, readAll(conn), io.EOF)
		}},
		{"close packet then Close", "PW", "transport close", "", func(t *testing.T, f *logFixture) {
			conn := f.accept(t)
			f.send(t, eioparser.CLOSE, "")
			require.ErrorIs(t, readAll(conn), io.EOF)
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
			f.send(t, eioparser.MESSAGE, "hi")
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
				opts := []Option{WithLogger(slog.New(rec)), WithPingInterval(time.Millisecond)}
				opts = append(opts, WithPingTimeout(map[string]time.Duration{"ping timeout": 100 * time.Millisecond, "transport error": 400 * time.Millisecond}[tc.reason]))
				f := &logFixture{srv: NewServer(opts...)}
				ts := httptest.NewServer(f.srv)
				t.Cleanup(ts.Close)
				t.Cleanup(func() { _ = f.srv.Close() })
				f.url = ts.URL

				u, err := url.Parse(ts.URL + "/?EIO=3")
				require.NoError(t, err)
				if tr == "polling" {
					f.cl, err = polling.Default.Dial(u, nil)
					require.NoError(t, err)
					_, err = f.cl.(engineclient.Opener).Open()
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
