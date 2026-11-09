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
	"github.com/sshaplygin/go-socket.io/v2/engineio/client"
	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
	"github.com/sshaplygin/go-socket.io/v2/engineio/internal/logtest"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/websocket"
)

// dialRaw opens a session on tr against url at the transport level: the connection
// neither answers PING nor sends anything the test does not write.
func dialRaw(t *testing.T, tr transport.Transport, rawURL string) transport.Conn {
	t.Helper()
	u, err := url.Parse(rawURL + "/?EIO=4")
	require.NoError(t, err)
	cl, err := tr.Dial(u, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	if o, ok := cl.(client.Opener); ok {
		_, err = o.Open()
	} else {
		var pt packet.Type
		var r io.ReadCloser
		_, pt, r, err = cl.NextReader()
		require.NoError(t, err)
		require.Equal(t, packet.OPEN, pt)
		err = r.Close()
	}
	require.NoError(t, err)
	return cl
}

// TestServerHeartbeat runs the Engine.IO v4 heartbeat over each transport: the server
// sends PING (`2`) PingInterval after the handshake and after each PONG, a client that
// answers with PONG (`3`) keeps the session open past several PingTimeouts, and the
// first unanswered PING closes it with reason "ping timeout" after PingTimeout.
func TestServerHeartbeat(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			const interval, timeout = 20 * time.Millisecond, 300 * time.Millisecond
			rec := logtest.NewRecorder()
			srv := engineio.NewServer(&engineio.Options{
				Logger: slog.New(rec), PingInterval: interval, PingTimeout: timeout,
			})
			t.Cleanup(func() { _ = srv.Close() })
			ts := httptest.NewServer(srv)
			t.Cleanup(ts.Close)

			cl := dialRaw(t, tr, ts.URL)
			conn, err := srv.Accept()
			require.NoError(t, err)
			go func() { _ = readAll(conn) }()

			for i := 0; i < 3; i++ {
				require.NoError(t, cl.SetReadDeadline(time.Now().Add(5*time.Second)))
				ft, pt, r, err := cl.NextReader()
				require.NoError(t, err)
				data, err := io.ReadAll(r)
				require.NoError(t, err)
				require.NoError(t, r.Close())
				require.Equal(t, frame.String, ft)
				require.Equal(t, packet.PING, pt)
				require.Empty(t, data)

				w, err := cl.NextWriter(frame.String, packet.PONG)
				require.NoError(t, err)
				require.NoError(t, w.Close())
			}
			require.Empty(t, rec.Find("engineio: session close"), "answered PINGs keep the session open")
			require.Equal(t, 1, srv.Count())

			require.NoError(t, cl.SetReadDeadline(time.Now().Add(5*time.Second)))
			_, pt, r, err := cl.NextReader()
			require.NoError(t, err)
			require.Equal(t, packet.PING, pt)
			require.NoError(t, r.Close())
			require.Eventually(t, func() bool { return len(rec.Find("engineio: session close")) == 1 },
				5*time.Second, time.Millisecond)
			require.Equal(t, "ping timeout", rec.Find("engineio: session close")[0]["reason"])
		})
	}
}
