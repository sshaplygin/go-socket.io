package client

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// Covers K16
func TestEngineClientWritable(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			engine := engineio.NewServer(engineio.WithPingInterval(10*time.Second), engineio.WithPingTimeout(10*time.Second))
			defer func() { _ = engine.Close() }()
			posting, release := make(chan struct{}, 1), make(chan struct{})
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					// Let the server consume the body, then keep the response in flight.
					recorder := httptest.NewRecorder()
					engine.ServeHTTP(recorder, r)
					posting <- struct{}{}
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					for key, values := range recorder.Header() {
						w.Header()[key] = values
					}
					w.WriteHeader(recorder.Code)
					_, _ = w.Write(recorder.Body.Bytes())
					return
				}
				engine.ServeHTTP(w, r)
			}))
			defer func() { _ = engine.Close(); ts.Close() }()
			defer close(release)
			accepted := make(chan engineio.Conn, 1)
			go func() {
				c, err := engine.Accept()
				if err != nil {
					return
				}
				accepted <- c
				for {
					_, body, err := c.NextReader()
					if err != nil {
						return
					}
					_, _ = io.Copy(io.Discard, body)
					_ = body.Close()
				}
			}()
			dialer := Dialer{Transports: []transport.Transport{tr}}
			c, err := dialer.Dial(ts.URL, nil)
			require.NoError(t, err)
			defer func() { _ = c.Close() }()
			select {
			case <-accepted:
			case <-time.After(time.Second):
				t.Fatal("server did not accept")
			}
			writable, ok := c.(interface{ Writable() bool })
			require.True(t, ok)
			require.True(t, writable.Writable())
			w, err := c.NextWriter(eioparser.String)
			require.NoError(t, err)
			require.False(t, writable.Writable())
			_, err = w.Write([]byte("hello"))
			require.NoError(t, err)
			require.NoError(t, w.Close())
			require.NoError(t, w.Close())
			if tr.Name() == "polling" {
				select {
				case <-posting:
				case <-time.After(time.Second):
					t.Fatal("POST did not reach server")
				}
				require.False(t, writable.Writable(), "POST response is still pending")
				release <- struct{}{}
				require.Eventually(t, writable.Writable, time.Second, time.Millisecond)
			} else {
				require.True(t, writable.Writable())
			}
			require.NoError(t, c.Close())
			require.False(t, writable.Writable())
		})
	}
}
