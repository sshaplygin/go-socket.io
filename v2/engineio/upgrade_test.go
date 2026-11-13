package engineio_test

import (
	"io"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio"
	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
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
