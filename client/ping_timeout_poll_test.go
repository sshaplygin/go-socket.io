package client_test

import (
	"bytes"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

// slowHeartbeat makes a polling client believe in a slower heartbeat than the server
// keeps: it rewrites the OPEN packet's 100 ms ping interval and timeout to 999 ms, so
// the server's window expires before the client's first PING. It records whether a
// poll found its session already unregistered.
type slowHeartbeat struct {
	next http.RoundTripper
	late atomic.Bool
}

func (s *slowHeartbeat) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := s.next.RoundTrip(r)
	if err != nil || r.Method != http.MethodGet {
		return resp, err
	}
	if resp.StatusCode == http.StatusBadRequest {
		s.late.Store(true)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	body = bytes.Replace(body, []byte(`"pingInterval":100,"pingTimeout":100`), []byte(`"pingInterval":999,"pingTimeout":999`), 1)
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

// A Go polling client that stops pinging has its pending poll answered with CLOSE when
// the server's heartbeat window expires, as engine.io 3.6.2 does, and reports
// "transport close"; the server reports "ping timeout". Neither calls OnError.
func TestPollingClientAtServerPingTimeoutReportsTransportClose(t *testing.T) {
	for attempt := 1; ; attempt++ {
		rt := &slowHeartbeat{next: http.DefaultTransport.(*http.Transport).Clone()}
		serverReasons, clientReasons, errs := make(chan string, 1), make(chan string, 1), make(chan string, 8)
		ts := newTestServer(t, func(s *socketio.Server) {
			s.OnConnect("/", func(socketio.Conn) error { return nil })
			s.OnError("/", func(_ socketio.Conn, err error) { errs <- "server: " + err.Error() })
			s.OnDisconnect("/", func(_ socketio.Conn, reason string) { serverReasons <- reason })
		}, engineio.WithPingInterval(100*time.Millisecond), engineio.WithPingTimeout(100*time.Millisecond))
		tr := &polling.Transport{Client: &http.Client{Transport: rt, Timeout: 10 * time.Second}}
		cl, err := client.New(ts.URL, client.WithTransports(tr), client.WithReconnection(false))
		require.NoError(t, err)
		cl.OnError(func(_ client.Conn, err error) { errs <- "client: " + err.Error() })
		cl.OnDisconnect(func(_ client.Conn, reason string) { clientReasons <- reason })
		require.NoError(t, cl.Connect())

		require.Equal(t, "ping timeout", recv(t, serverReasons, "server OnDisconnect"))
		reason := recv(t, clientReasons, "client OnDisconnect")
		_ = cl.Close()
		if reason == "transport error" && rt.late.Load() && attempt < 5 {
			// The client's next poll arrived after the session had ended, so no
			// poll was pending at the timeout; that run says nothing.
			continue
		}
		require.Equal(t, "transport close", reason)
		require.Zero(t, len(errs), "OnError was called")
		return
	}
}
