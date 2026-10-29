package client_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

// longPingServer starts a Socket.IO server whose heartbeat window (2 h) no test
// reaches, so only the client's CLOSE packet or a canceled poll ends a session.
func longPingServer(t *testing.T, reasons chan<- string) (*socketio.Server, *httptest.Server) {
	t.Helper()
	srv := socketio.NewServer(socketio.WithEngineOptions(
		engineio.WithPingInterval(time.Hour), engineio.WithPingTimeout(time.Hour),
		engineio.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))))
	srv.OnConnect("/", func(socketio.Conn) error { return nil })
	srv.OnDisconnect("/", func(_ socketio.Conn, reason string) { reasons <- reason })
	go func() { _ = srv.Serve() }()
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { _ = srv.Close(); ts.Close() })
	return srv, ts
}

func connectPolling(t *testing.T, url string, tr *polling.Transport) *client.Client {
	t.Helper()
	cl, err := client.New(url, client.WithTransports(tr), client.WithReconnection(false))
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	connected := make(chan struct{}, 1)
	cl.OnConnect(func(client.Conn) error { connected <- struct{}{}; return nil })
	require.NoError(t, cl.Connect())
	recv(t, connected, "client CONNECT")
	return cl
}

// detachedPolls forwards long-poll GETs without their cancellation, as a proxy
// that does not pass on a client abort: the server cannot learn of Close from a
// canceled GET, which otherwise happens only when one is pending at Close.
func detachedPolls(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet {
		return http.DefaultTransport.RoundTrip(r)
	}
	type result struct {
		resp *http.Response
		err  error
	}
	forwarded := make(chan result, 1)
	go func() {
		resp, err := http.DefaultTransport.RoundTrip(r.Clone(context.WithoutCancel(r.Context())))
		forwarded <- result{resp, err}
	}()
	select {
	case res := <-forwarded:
		return res.resp, res.err
	case <-r.Context().Done():
		go func() {
			if res := <-forwarded; res.resp != nil {
				_ = res.resp.Body.Close()
			}
		}()
		return nil, r.Context().Err()
	}
}

func TestPollingCloseEndsServerSessions(t *testing.T) {
	reasons := make(chan string, 1)
	srv, ts := longPingServer(t, reasons)
	tr := &polling.Transport{Client: &http.Client{Transport: closeRoundTripper(detachedPolls)}}
	for i := 0; i < 20; i++ {
		// Close right after Connect, while handshake requests may be in flight.
		cl, err := client.New(ts.URL, client.WithTransports(tr), client.WithReconnection(false))
		require.NoError(t, err)
		require.NoError(t, cl.Connect())
		require.NoError(t, cl.Close())
		require.Equal(t, "client namespace disconnect", recv(t, reasons, "server OnDisconnect"))
	}
	require.Eventually(t, func() bool { return srv.Count() == 0 }, 2*time.Second, 5*time.Millisecond,
		"closed clients still hold server sessions until PingInterval+PingTimeout")
}

// clientGoroutines returns, by ID, the stacks of goroutines running the
// Socket.IO or Engine.IO client or its polling connection.
func clientGoroutines() map[string]string {
	buffer := make([]byte, 1<<22)
	buffer = buffer[:runtime.Stack(buffer, true)]
	ids := map[string]string{}
	for _, stack := range strings.Split(string(buffer), "\n\n") {
		for _, frame := range []string{"go-socket.io/client.", "engineio/client.", "polling.(*clientConn)"} {
			if strings.Contains(stack, frame) {
				ids[strings.Fields(stack)[1]] = stack
			}
		}
	}
	return ids
}

func TestPollingCloseUnansweredClosePacket(t *testing.T) {
	reasons := make(chan string, 1)
	_, ts := longPingServer(t, reasons)
	before := clientGoroutines()
	entered := make(chan struct{})
	tr := &polling.Transport{Client: &http.Client{Transport: closeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.Body != nil {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			if bytes.Equal(body, []byte{0x00, 0x01, 0xff, '1'}) { // CLOSE in a binary payload
				close(entered)
				<-r.Context().Done() // the server never answers
				return nil, r.Context().Err()
			}
			r = r.Clone(r.Context())
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		return http.DefaultTransport.RoundTrip(r)
	})}}
	cl := connectPolling(t, ts.URL, tr)

	started := time.Now()
	require.NoError(t, cl.Close())
	require.Less(t, time.Since(started), 100*time.Millisecond, "Close waited for the transport")
	require.Equal(t, "client namespace disconnect", recv(t, reasons, "server OnDisconnect"))
	recv(t, entered, "the CLOSE packet POST")
	deadline := started.Add(3 * time.Second)
	leftover := func() (stacks []string) {
		for id, stack := range clientGoroutines() {
			if _, ok := before[id]; !ok {
				stacks = append(stacks, stack)
			}
		}
		return stacks
	}
	for len(leftover()) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	require.Empty(t, leftover(), "client goroutines outlived the unanswered CLOSE POST by 3 s")
}
