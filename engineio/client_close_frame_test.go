package engineio

import (
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// The Go engine client's Close sends a normal closure frame, so the session ends
// with "transport close", not the "transport error" of a drop; also after an upgrade.
func TestGoClientCloseIsTransportClose(t *testing.T) {
	for name, transports := range map[string][]transport.Transport{"websocket": {websocket.Default}, "upgraded": {polling.Default, websocket.Default}} {
		t.Run(name, func(t *testing.T) {
			srv := NewServer()
			ts := httptest.NewServer(srv)
			defer func() { _ = srv.Close(); ts.Close() }()
			upgrade := len(transports) > 1
			cl, err := (&engineclient.Dialer{Transports: transports, Upgrade: upgrade}).Dial(ts.URL, nil)
			require.NoError(t, err)
			conn, err := srv.Accept()
			require.NoError(t, err)
			go func() { // a polling client reads while it pauses for the switch
				for _, r, err := cl.NextReader(); err == nil; _, r, err = cl.NextReader() {
					_ = r.Close()
				}
			}()
			require.Eventually(t, func() bool { return cl.Upgraded() == upgrade && conn.Upgraded() == upgrade }, 5*time.Second, time.Millisecond)
			read := make(chan error, 1) // the server's reader answers the close frame
			go func() { read <- readAll(conn) }()
			require.NoError(t, cl.Close())
			var closed *CloseError
			err = <-read
			require.ErrorAs(t, err, &closed)
			require.Equal(t, ReasonTransportClose, closed.Reason, "%v", err)
			require.ErrorIs(t, err, io.EOF)
		})
	}
}
