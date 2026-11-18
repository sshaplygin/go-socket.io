package client_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// silentEndpoint accepts Engine.IO connections and never sends OPEN. It
// reports on started each time a request arrives, waits delay before it
// answers, and reports on ended each time a connection it accepted ends.
type silentEndpoint struct {
	started chan struct{}
	ended   chan struct{}
	delay   time.Duration
	wg      sync.WaitGroup
	done    chan struct{}
}

func newSilentEndpoint(t *testing.T, delay time.Duration) (*silentEndpoint, *httptest.Server) {
	t.Helper()
	e := &silentEndpoint{started: make(chan struct{}, 64), ended: make(chan struct{}, 64), delay: delay, done: make(chan struct{})}
	srv := httptest.NewServer(e)
	t.Cleanup(func() {
		close(e.done)
		srv.CloseClientConnections()
		srv.Close()
		e.wg.Wait()
	})
	return e, srv
}

func (e *silentEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.wg.Add(1)
	defer e.wg.Done()
	defer func() { e.ended <- struct{}{} }()
	e.started <- struct{}{}
	select {
	case <-time.After(e.delay):
	case <-e.done:
		return
	}
	if r.URL.Query().Get("transport") != "websocket" {
		select {
		case <-r.Context().Done():
		case <-e.done:
		}
		return
	}
	ws, err := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = ws.Close() }()
	go func() {
		<-e.done
		_ = ws.Close()
	}()
	for {
		if _, _, err := ws.NextReader(); err != nil {
			return
		}
	}
}

// TestConnectTimeoutAbortsDial checks that a Connect that times out before the
// server sends OPEN does not leave its dial running: once the client is
// closed, the server sees every dialed connection end.
func TestConnectTimeoutAbortsDial(t *testing.T) {
	for _, tc := range []struct {
		name      string
		transport transport.Transport
	}{
		{"websocket", &websocket.Transport{HandshakeTimeout: time.Second}},
		{"polling", &polling.Transport{Client: &http.Client{Timeout: time.Minute}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, srv := newSilentEndpoint(t, 0)
			cl, err := client.New(srv.URL, client.WithTransports(tc.transport),
				client.WithTimeout(100*time.Millisecond), client.WithReconnection(false))
			require.NoError(t, err)
			const attempts = 3
			for i := 0; i < attempts; i++ {
				require.ErrorIs(t, cl.Connect(), client.ErrConnectTimeout)
			}
			require.NoError(t, cl.Close())

			deadline := time.After(time.Second)
			for i := 0; i < attempts; i++ {
				select {
				case <-endpoint.ended:
				case <-deadline:
					t.Fatalf("%d of %d timed-out dials still open 1s after Close", attempts-i, attempts)
				}
			}
		})
	}
}

// TestCloseAbortsDial checks that Client.Close, with no connect timeout, ends
// a Connect whose dial waits for OPEN and closes that dial.
func TestCloseAbortsDial(t *testing.T) {
	for _, tc := range []struct {
		name      string
		transport transport.Transport
	}{
		{"websocket", &websocket.Transport{HandshakeTimeout: time.Second}},
		{"polling", &polling.Transport{Client: &http.Client{Timeout: time.Minute}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, srv := newSilentEndpoint(t, 0)
			cl, err := client.New(srv.URL, client.WithTransports(tc.transport),
				client.WithTimeout(-1), client.WithReconnection(false))
			require.NoError(t, err)
			connected := make(chan error, 1)
			go func() { connected <- cl.Connect() }()
			select {
			case <-endpoint.started:
			case <-time.After(2 * time.Second):
				t.Fatal("no dial reached the server")
			}
			require.NoError(t, cl.Close())
			select {
			case err := <-connected:
				require.Error(t, err)
			case <-time.After(time.Second):
				t.Fatal("Connect did not return 1s after Close")
			}
			select {
			case <-endpoint.ended:
			case <-time.After(time.Second):
				t.Fatal("dial still open 1s after Close")
			}
		})
	}
}

// TestConnectTimeoutAbortsLateHandshake checks that a websocket handshake that
// completes only after the connect attempt timed out is closed at once.
func TestConnectTimeoutAbortsLateHandshake(t *testing.T) {
	endpoint, srv := newSilentEndpoint(t, 300*time.Millisecond)
	cl, err := client.New(srv.URL, client.WithTransports(&websocket.Transport{HandshakeTimeout: time.Second}),
		client.WithTimeout(100*time.Millisecond), client.WithReconnection(false))
	require.NoError(t, err)
	require.ErrorIs(t, cl.Connect(), client.ErrConnectTimeout)
	require.NoError(t, cl.Close())
	select {
	case <-endpoint.ended:
	case <-time.After(2 * time.Second):
		t.Fatal("late websocket dial still open 2s after Close")
	}
}
