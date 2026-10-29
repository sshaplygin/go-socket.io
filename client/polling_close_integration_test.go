package client_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/stretchr/testify/require"
)

type closeRoundTripper func(*http.Request) (*http.Response, error)

func (f closeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPollingCloseDrainsLastPost(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "drain"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			srv := socketio.NewServer()
			disconnected := make(chan struct{}, 1)
			srv.OnConnect("/", func(socketio.Conn) error { return nil })
			srv.OnDisconnect("/", func(socketio.Conn, string) { disconnected <- struct{}{} })
			ts := httptest.NewServer(srv)
			defer ts.Close()
			defer func() { _ = srv.Close() }()
			go func() { _ = srv.Serve() }()
			entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			var armed atomic.Bool
			tr := &polling.Transport{Client: &http.Client{Transport: closeRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodPost && armed.CompareAndSwap(true, false) {
					close(entered)
					select {
					case <-release:
					case <-r.Context().Done():
						close(canceled)
						return nil, r.Context().Err()
					}
				}
				return http.DefaultTransport.RoundTrip(r)
			})}}
			timeout := time.Second
			if deadline {
				timeout = 50 * time.Millisecond
			}
			cl, err := client.New(ts.URL, client.WithTransports(tr), client.WithReconnection(false), client.WithCloseTimeout(timeout))
			require.NoError(t, err)
			defer func() { _ = cl.Close() }()
			connected := make(chan struct{}, 1)
			cl.OnConnect(func(client.Conn) error { connected <- struct{}{}; return nil })
			require.NoError(t, cl.Connect())
			select {
			case <-connected:
			case <-time.After(time.Second):
				t.Fatal("no CONNECT")
			}
			armed.Store(true)
			require.NoError(t, cl.Close())
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("no disconnect POST")
			}
			if deadline {
				// After the close deadline, Engine.IO waits up to 2 s for the
				// in-flight POST so that CLOSE can follow it, then cancels it.
				select {
				case <-canceled:
				case <-time.After(3 * time.Second):
					t.Fatal("close deadline did not cancel POST")
				}
				return
			}
			select {
			case <-canceled:
				t.Fatal("orderly close canceled DISCONNECT before its POST completed")
			case <-time.After(25 * time.Millisecond):
			}
			unblock()
			select {
			case <-disconnected:
			case <-time.After(time.Second):
				t.Fatal("server did not receive DISCONNECT")
			}
		})
	}
}
