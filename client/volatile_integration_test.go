package client_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

type watchedClientTransport struct {
	transport.Transport
	opened chan transport.Conn
}

func (w watchedClientTransport) Dial(u *url.URL, h http.Header) (transport.Conn, error) {
	c, err := w.Transport.Dial(u, h)
	if err == nil {
		w.opened <- c
	}
	return c, err
}

// Covers C11
func TestClientVolatile(t *testing.T) {
	for _, name := range []string{"polling", "websocket"} {
		t.Run(name, func(t *testing.T) {
			opts := shortPingOptions()
			opts = append(opts, engineio.WithPingInterval(time.Hour)) // no heartbeat can race this idle admission
			srv := socketio.NewServer(socketio.WithEngineOptions(opts...))
			values := make(chan string, 8)
			srv.OnConnect("/", func(socketio.Conn) error { return nil })
			srv.OnEvent("/", "value", func(_ socketio.Conn, v string) string { values <- v; return v })
			go func() { _ = srv.Serve() }()
			var armed atomic.Bool
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hold := r.Method == http.MethodPost && armed.CompareAndSwap(true, false)
				srv.ServeHTTP(w, r)
				if hold {
					close(entered)
					<-release
				}
			}))
			t.Cleanup(func() { unblock(); _ = srv.Close(); ts.Close() })
			var base transport.Transport = polling.Default
			if name == "websocket" {
				base = websocket.Default
			}
			watched := watchedClientTransport{base, make(chan transport.Conn, 1)}
			cl, err := client.New(ts.URL, client.WithTransports([]transport.Transport{watched}...))
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			connected := make(chan client.Conn, 1)
			cl.OnConnect(func(c client.Conn) error { connected <- c; return nil })
			require.NoError(t, cl.Connect())
			c := recv(t, connected, "connect")
			wire := recv(t, watched.opened, "transport")
			if name == "polling" {
				armed.Store(true)
				c.Emit("value", "held")
				recv(t, entered, "held POST response")
				require.False(t, wire.(interface{ Writable() bool }).Writable())
				require.NoError(t, c.Volatile().Emit("value", "drop"))
				acked := make(chan string, 1)
				c.Emit("value", "barrier", func(v string) { acked <- v })
				unblock()
				require.Equal(t, "held", recv(t, values, "first POST"))
				require.Equal(t, "barrier", recv(t, values, "packet after dropped volatile"))
				require.Equal(t, "barrier", recv(t, acked, "barrier ACK"))
			}
			// Wait for both the POST response and the engine writer to finish.
			deadline := time.Now().Add(waitFor)
			for !client.ClientWritableForTest(cl) {
				if time.Now().After(deadline) {
					t.Fatal("engine stayed busy")
				}
				runtime.Gosched()
			}
			acked := make(chan string, 1)
			require.NoError(t, c.Volatile().Emit("value", "idle", func(v string) { acked <- v }))
			require.Equal(t, "idle", recv(t, values, "idle volatile delivery"))
			require.Equal(t, "idle", recv(t, acked, "idle volatile ACK"))
			require.Empty(t, values)
		})
	}
}
