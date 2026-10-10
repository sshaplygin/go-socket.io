package engineio_test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/internal/logtest"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// logFixture is an engineio.Server with a recording logger and a client
// dialled at the transport level, so that it sends no pings.
type logFixture struct {
	srv *engineio.Server
	url string
	cl  transport.Conn
}

func (f *logFixture) accept(t *testing.T) engineio.Conn {
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
func readAll(conn engineio.Conn) error {
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
func (f *logFixture) upgrade(t *testing.T, conn engineio.Conn) {
	u, err := url.Parse(f.url + "/?EIO=4&sid=" + conn.ID())
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
			require.Eventually(t, func() bool { return engineio.ConnChanLen(f.srv) == 1 }, time.Second, time.Millisecond)
			require.NoError(t, f.srv.Close())
		}},
	}
	for _, tc := range cases {
		for _, side := range tc.sides {
			tr := map[rune]string{'P': "polling", 'W': "websocket"}[side]
			t.Run(tc.name+"/"+tr, func(t *testing.T) {
				rec := logtest.NewRecorder()
				opts := &engineio.Options{Logger: slog.New(rec)}
				opts.PingTimeout = map[string]time.Duration{"ping timeout": 100 * time.Millisecond, "transport error": 400 * time.Millisecond}[tc.reason]
				f := &logFixture{srv: engineio.NewServer(opts)}
				ts := httptest.NewServer(f.srv)
				t.Cleanup(ts.Close)
				t.Cleanup(func() { _ = f.srv.Close() })
				f.url = ts.URL

				u, err := url.Parse(ts.URL + "/?EIO=4")
				require.NoError(t, err)
				if tr == "polling" {
					f.cl, err = polling.Default.Dial(u, nil)
					require.NoError(t, err)
					_, err = f.cl.(client.Opener).Open()
				} else {
					f.cl, err = websocket.Default.Dial(u, nil)
				}
				require.NoError(t, err)
				cl := f.cl
				t.Cleanup(func() { _ = cl.Close() })

				tc.run(t, f)
				closes := rec.Find("engineio: session close")
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
