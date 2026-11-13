package client_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// When the server ends the session, neither the Go client's nor the server's OnError is
// called; each OnDisconnect gets the reason. A polling client sees transport error after
// Server.Close because the server answers its pending poll with HTTP 500.
func TestServerSessionEndSkipsOnError(t *testing.T) {
	for _, tc := range []struct {
		name     string
		trs      []transport.Transport
		shutdown string // the client's reason after Server.Close
	}{
		{"polling", []transport.Transport{polling.Default}, "transport error"},
		{"websocket", []transport.Transport{websocket.Default}, "transport close"},
		{"upgrade", []transport.Transport{polling.Default, websocket.Default}, "transport close"},
	} {
		for _, call := range []string{"Server.Close", "EngineConn().Close()"} {
			t.Run(tc.name+"/"+call, func(t *testing.T) {
				var srv *socketio.Server
				conns, errs := make(chan socketio.Conn, 1), make(chan string, 8)
				serverReasons, clientReasons := make(chan string, 1), make(chan string, 1)
				ts := newTestServer(t, func(s *socketio.Server) {
					srv = s
					s.OnConnect("/", func(c socketio.Conn) error { conns <- c; return nil })
					s.OnError("/", func(_ socketio.Conn, err error) { errs <- "server: " + err.Error() })
					s.OnDisconnect("/", func(_ socketio.Conn, reason string) { serverReasons <- reason })
				}, steadyPing...)
				cl, err := client.New(ts.URL, client.WithTransports(tc.trs...), client.WithReconnection(false))
				require.NoError(t, err)
				t.Cleanup(func() { _ = cl.Close() })
				cl.OnError(func(_ client.Conn, err error) { errs <- "client: " + err.Error() })
				cl.OnDisconnect(func(_ client.Conn, reason string) { clientReasons <- reason })
				require.NoError(t, cl.Connect())
				c := recv(t, conns, "server OnConnect")
				want := tc.trs[len(tc.trs)-1].Name()
				require.Eventually(t, func() bool { return c.EngineConn().Transport() == want }, waitFor, time.Millisecond)
				reason := "transport close"
				if call == "Server.Close" {
					reason = tc.shutdown
					require.NoError(t, srv.Close())
				} else {
					require.NoError(t, c.EngineConn().Close())
				}
				require.Equal(t, "forced close", recv(t, serverReasons, "server OnDisconnect"))
				// A report, if any, would reach OnError before OnDisconnect, so the check is complete.
				require.Equal(t, reason, recv(t, clientReasons, "client OnDisconnect"))
				require.NoError(t, srv.Close()) // waits for the server connection
				require.Zero(t, len(errs), "OnError was called")
			})
		}
	}
}
