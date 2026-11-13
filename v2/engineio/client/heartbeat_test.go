package client

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio"
	"github.com/sshaplygin/go-socket.io/v2/engineio/internal/logtest"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/websocket"
)

// TestClientAnswersServerPing: a Dialer client answers the PINGs of an Engine.IO v4
// server with PONG and sends no PING itself, so the server keeps the session open for
// many intervals. A missing PONG would close it with "ping timeout", a client PING
// with "transport error"; either is a close record.
func TestClientAnswersServerPing(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			const interval, timeout = 10 * time.Millisecond, 500 * time.Millisecond
			rec := logtest.NewRecorder()
			srv := engineio.NewServer(&engineio.Options{
				Logger: slog.New(rec), PingInterval: interval, PingTimeout: timeout,
				Transports: []transport.Transport{tr},
			})
			t.Cleanup(func() { _ = srv.Close() })
			ts := httptest.NewServer(srv)
			t.Cleanup(ts.Close)

			cl, err := (&Dialer{Transports: []transport.Transport{tr}}).Dial(ts.URL, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			go func() { _ = readAll(cl) }()
			conn, err := srv.Accept()
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			go func() { _ = readAll(conn) }()

			time.Sleep(timeout + 20*interval) // past the first ping timeout and many PINGs
			require.Empty(t, rec.Find("engineio: session close"))
			require.Equal(t, 1, srv.Count())
		})
	}
}

// silentServer answers the handshake on either transport with an OPEN packet that
// announces pingInterval and pingTimeout in milliseconds, then never sends a PING:
// polls wait until the client gives up and the websocket is read until it closes.
func silentServer(t *testing.T, interval, timeout int) *httptest.Server {
	open := `{"sid":"s","upgrades":[],"pingInterval":` + itoa(interval) + `,"pingTimeout":` + itoa(timeout) + `}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("transport") == "websocket" {
			c, _, _, err := ws.UpgradeHTTP(r, w)
			if err != nil {
				return
			}
			defer func() { _ = c.Close() }()
			_ = wsutil.WriteServerText(c, []byte("0"+open))
			_, _ = io.Copy(io.Discard, c)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		switch {
		case q.Get("sid") == "":
			_, _ = w.Write([]byte("0" + open))
		case r.Method == http.MethodGet:
			<-r.Context().Done()
		default:
			_, _ = w.Write([]byte("ok"))
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// TestClientPingTimeout: a client that receives no PING for pingInterval + pingTimeout
// of the OPEN packet closes the session; NextReader then fails with "ping timeout".
func TestClientPingTimeout(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			const interval, timeout = 20, 30 // milliseconds
			ts := silentServer(t, interval, timeout)
			start := time.Now()
			cl, err := (&Dialer{Transports: []transport.Transport{tr}}).Dial(ts.URL, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })

			errs := make(chan error, 1)
			go func() { errs <- readAll(cl) }()
			select {
			case err := <-errs:
				require.ErrorContains(t, err, "ping timeout")
				require.GreaterOrEqual(t, time.Since(start), (interval+timeout)*time.Millisecond)
			case <-time.After(5 * time.Second):
				t.Fatal("the client did not close without server PINGs")
			}
		})
	}
}
