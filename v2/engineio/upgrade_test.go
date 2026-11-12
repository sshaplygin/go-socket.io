package engineio_test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
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

// writePacket writes one packet through a transport-level connection.
func writePacket(t *testing.T, c transport.Conn, pt packet.Type, data string) {
	t.Helper()
	w, err := c.NextWriter(frame.String, pt)
	require.NoError(t, err)
	_, err = w.Write([]byte(data))
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

// readPacket reads the next packet of c that is not a NOOP, within five seconds.
func readPacket(t *testing.T, c transport.Conn) (packet.Type, string) {
	t.Helper()
	require.NoError(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
	for {
		_, pt, r, err := c.NextReader()
		require.NoError(t, err)
		b, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		if pt != packet.NOOP {
			return pt, string(b)
		}
	}
}

// TestUpgradeKeepsPollingUntilUpgradePacket: between the probe and UPGRADE (`5`) the
// session still runs on polling: a message the server writes then is delivered to
// the polling client. Only UPGRADE pauses polling and moves the session to websocket.
func TestUpgradeKeepsPollingUntilUpgradePacket(t *testing.T) {
	srv := engineio.NewServer(nil)
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	p := dialRaw(t, polling.Default, ts.URL)
	conn, err := srv.Accept()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	u, err := url.Parse(ts.URL + "/?EIO=4&sid=" + conn.ID())
	require.NoError(t, err)
	ws, err := websocket.Default.Dial(u, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	writePacket(t, ws, packet.PING, "probe")
	pt, data := readPacket(t, ws)
	require.Equal(t, packet.PONG, pt)
	require.Equal(t, "probe", data)

	written := make(chan error, 1)
	go func() {
		w, err := conn.NextWriter(frame.String)
		if err == nil {
			_, err = w.Write([]byte("during upgrade"))
		}
		if err == nil {
			err = w.Close()
		}
		written <- err
	}()
	pt, data = readPacket(t, p)
	require.Equal(t, packet.MESSAGE, pt)
	require.Equal(t, "during upgrade", data)
	require.NoError(t, <-written)
	require.Equal(t, "polling", conn.(*session.Session).Transport())

	writePacket(t, ws, packet.UPGRADE, "")
	require.Eventually(t, func() bool { return conn.(*session.Session).Transport() == "websocket" },
		5*time.Second, time.Millisecond)
}

// nextType reads the next packet of c and returns its type; the read fails after d.
func nextType(c transport.Conn, d time.Duration) (packet.Type, error) {
	if err := c.SetReadDeadline(time.Now().Add(d)); err != nil {
		return 0, err
	}
	_, pt, r, err := c.NextReader()
	if err != nil {
		return 0, err
	}
	return pt, r.Close()
}

// TestUpgradeAnswersEveryPollWithNoop: from the probe until UPGRADE or a failed
// upgrade, every poll of the polling connection is answered with NOOP, a poll sent
// after an earlier NOOP too, at most 100 ms apart as on the Node server (here within a
// second, far below the 20 s pingInterval). After a failed upgrade the NOOPs stop: the
// polls carry the session's messages again, and at most one NOOP that was already
// waiting follows them.
func TestUpgradeAnswersEveryPollWithNoop(t *testing.T) {
	for _, upgrade := range []bool{true, false} {
		t.Run(map[bool]string{true: "upgrade", false: "failed upgrade"}[upgrade], func(t *testing.T) {
			rec := logtest.NewRecorder()
			srv := engineio.NewServer(&engineio.Options{Logger: slog.New(rec)})
			t.Cleanup(func() { _ = srv.Close() })
			ts := httptest.NewServer(srv)
			t.Cleanup(ts.Close)
			p := dialRaw(t, polling.Default, ts.URL)
			conn, err := srv.Accept()
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })

			u, err := url.Parse(ts.URL + "/?EIO=4&sid=" + conn.ID())
			require.NoError(t, err)
			ws, err := websocket.Default.Dial(u, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = ws.Close() })
			writePacket(t, ws, packet.PING, "probe")
			pt, _ := readPacket(t, ws)
			require.Equal(t, packet.PONG, pt)

			for i := 0; i < 3; i++ { // the pending poll, then the polls that follow a NOOP
				pt, err := nextType(p, time.Second)
				require.NoError(t, err, "poll %d", i)
				require.Equal(t, packet.NOOP, pt, "poll %d", i)
			}

			if upgrade {
				writePacket(t, ws, packet.UPGRADE, "")
				require.Eventually(t, func() bool { return conn.(*session.Session).Transport() == "websocket" },
					5*time.Second, time.Millisecond)
				return
			}
			require.NoError(t, ws.Close())
			require.Eventually(t, func() bool { return len(rec.Find("engineio: upgrade probe failed")) == 1 },
				5*time.Second, time.Millisecond)
			go func() {
				if w, err := conn.NextWriter(frame.String); err == nil {
					_ = w.Close()
				}
			}()
			pt, _ = readPacket(t, p)
			require.Equal(t, packet.MESSAGE, pt)
			noops := 0
			for { // three NOOP intervals: a sender still running would answer three polls
				pt, err := nextType(p, 300*time.Millisecond)
				if err != nil {
					break
				}
				require.Equal(t, packet.NOOP, pt)
				noops++
			}
			require.LessOrEqual(t, noops, 1)
		})
	}
}

// TestHeartbeatAfterUpgrade: after the session moves from polling to websocket the
// server sends its PINGs on websocket, PONGs there keep it open past several
// pingTimeouts, and an unanswered PING closes it once with "ping timeout". The
// interval is above the timeout, so a transport deadline of pingTimeout alone on the
// new connection would close the session before its first PING there.
func TestHeartbeatAfterUpgrade(t *testing.T) {
	const interval, timeout = 600 * time.Millisecond, 300 * time.Millisecond
	rec := logtest.NewRecorder()
	srv := engineio.NewServer(&engineio.Options{Logger: slog.New(rec), PingInterval: interval, PingTimeout: timeout})
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	p := dialRaw(t, polling.Default, ts.URL)
	conn, err := srv.Accept()
	require.NoError(t, err)
	go func() { _ = readAll(conn) }()

	u, err := url.Parse(ts.URL + "/?EIO=4&sid=" + conn.ID())
	require.NoError(t, err)
	ws, err := websocket.Default.Dial(u, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	writePacket(t, ws, packet.PING, "probe")
	pt, _ := readPacket(t, ws)
	require.Equal(t, packet.PONG, pt)
	writePacket(t, ws, packet.UPGRADE, "")
	require.Eventually(t, func() bool { return conn.(*session.Session).Transport() == "websocket" },
		5*time.Second, time.Millisecond)
	require.NoError(t, p.Close())

	start := time.Now()
	for i := 0; i < 3; i++ {
		pt, _ := readPacket(t, ws)
		require.Equal(t, packet.PING, pt, "PING %d on websocket", i)
		writePacket(t, ws, packet.PONG, "")
	}
	require.GreaterOrEqual(t, time.Since(start), 2*interval)
	require.Empty(t, rec.Find("engineio: session close"), "answered PINGs keep the session open")

	pt, _ = readPacket(t, ws)
	require.Equal(t, packet.PING, pt)
	require.Eventually(t, func() bool { return len(rec.Find("engineio: session close")) == 1 },
		5*time.Second, time.Millisecond)
	m := rec.Find("engineio: session close")[0]
	require.Equal(t, "ping timeout", m["reason"])
	require.Equal(t, "websocket", m["transport"])
}
