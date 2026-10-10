package client

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio"
	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
	"github.com/sshaplygin/go-socket.io/v2/engineio/internal/logtest"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/websocket"
)

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

// TestDialFailureRecords dials through two failing polling transports and an invalid URL
// with slog.Default recording. The failure Dial does not return is one WARN; the errors
// it returns and the polling client's request failures, which reach its reader, are
// DEBUG; every message follows the 1.L pattern. Not parallel: it sets slog.Default.
func TestDialFailureRecords(t *testing.T) {
	rec := logtest.NewRecorder()
	logtest.SetDefault(t, rec)

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
	for _, m := range rec.Find("") {
		assert.Regexp(t, pattern, m["msg"])
		if m["level"] != slog.LevelDebug.String() {
			loud = append(loud, m)
		}
	}
	require.Len(t, loud, 1)
	assert.Equal(t, "engineio: transport dial failed", loud[0]["msg"])
	assert.Equal(t, slog.LevelWarn.String(), loud[0]["level"])
	assert.Equal(t, "polling", loud[0]["transport"])
	assert.Len(t, rec.Find("engineio: transport dial failed"), 2)
	assert.Len(t, rec.Find("engineio: parse url failed"), 1)
}

// TestClientPeerCloseRecords closes the accepted session of a Dialer client
// that does not close itself, or has a websocket peer send a close frame after OPEN,
// reads the client until NextReader fails and waits for its ping to fail, with
// slog.Default recording: the 1.L Levels rule makes a ping failure after a peer close
// expected closure, so no record is above DEBUG. Not parallel.
//
// Covers the 1.L Levels rule on the engine.io client (P, W); no 1L-T case.
func TestClientPeerCloseRecords(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			rec := logtest.NewRecorder()
			logtest.SetDefault(t, rec)
			srv := engineio.NewServer(&engineio.Options{PingInterval: 50 * time.Millisecond, Transports: []transport.Transport{tr}})
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
			require.Eventually(t, func() bool { return len(rec.Find("engineio: ping failed")) > 0 },
				5*time.Second, time.Millisecond)
			for _, m := range rec.Find("") {
				assert.Equal(t, slog.LevelDebug.String(), m["level"], "%v", m)
			}
		})
	}
	t.Run("websocket close frame", func(t *testing.T) {
		rec := logtest.NewRecorder()
		logtest.SetDefault(t, rec)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, _, _, err := ws.UpgradeHTTP(r, w)
			if err != nil {
				return
			}
			defer func() { _ = c.Close() }()
			open := `0{"sid":"x","upgrades":[],"pingInterval":50,"pingTimeout":5000}`
			_ = wsutil.WriteServerText(c, []byte(open))
			_ = ws.WriteFrame(c, ws.NewCloseFrame(ws.NewCloseFrameBody(ws.StatusNormalClosure, "")))
			_, _ = io.Copy(io.Discard, c)
		}))
		defer ts.Close()

		cl, err := (&Dialer{Transports: []transport.Transport{websocket.Default}}).Dial(ts.URL, nil)
		require.NoError(t, err)
		defer func() { _ = cl.Close() }()
		require.Error(t, readAll(cl))
		require.Eventually(t, func() bool { return len(rec.Find("engineio: ping failed")) > 0 },
			5*time.Second, time.Millisecond)
		for _, m := range rec.Find("") {
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
	rec := logtest.NewRecorder()
	logtest.SetDefault(t, rec)
	params := transport.ConnParameters{PingInterval: time.Millisecond}
	(&client{conn: &brokenConn{}, params: params, close: make(chan struct{})}).serve()
	recs := rec.Find("engineio: ping failed")
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
	rec := logtest.NewRecorder()
	logtest.SetDefault(t, rec)
	_, _, err := (&client{conn: &brokenConn{}, close: make(chan struct{})}).NextReader()
	require.ErrorIs(t, err, errBroken)
	require.Len(t, rec.Find("engineio: close reader failed"), 1)
	for _, m := range rec.Find("") {
		assert.Equal(t, slog.LevelDebug.String(), m["level"], "%v", m)
	}
}
