package client

import (
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func TestClientWebsocketUpgrade(t *testing.T) {
	for _, name := range []string{"default", "polling", "websocket"} {
		t.Run(name, func(t *testing.T) {
			srv := engineio.NewServer()
			handshake := make(chan string, 1)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("sid") == "" {
					handshake <- r.URL.Query().Get("transport")
				}
				srv.ServeHTTP(w, r)
			}))
			var opts []Option
			if name == "polling" {
				opts = append(opts, WithTransports(polling.Default))
			}
			if name == "websocket" {
				opts = append(opts, WithTransports(websocket.Default))
			}
			cl, err := New(ts.URL, opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close(); _ = srv.Close(); ts.Close() })
			ready, echoed := make(chan struct{}, 1), make(chan string, 1)
			cl.OnConnect(func(Conn) error { ready <- struct{}{}; return nil })
			cl.OnEvent("echo", func(_ Conn, value string) { echoed <- value })
			require.NoError(t, cl.Connect())
			server, err := srv.Accept()
			require.NoError(t, err)
			// Minimal Socket.IO peer: keep reading throughout the asynchronous probe.
			peerDone := make(chan struct{})
			go func() {
				defer close(peerDone)
				writer, err := server.NextWriter(eioparser.String)
				if err != nil {
					return
				}
				_, _ = io.WriteString(writer, "0")
				if writer.Close() != nil {
					return
				}
				for {
					frame, reader, err := server.NextReader()
					if err != nil {
						return
					}
					data, err := io.ReadAll(reader)
					_ = reader.Close()
					if err != nil {
						return
					}
					if !strings.HasPrefix(string(data), "2") {
						continue
					}
					writer, err := server.NextWriter(frame)
					if err != nil {
						return
					}
					_, _ = writer.Write(data)
					if writer.Close() != nil {
						return
					}
				}
			}()
			t.Cleanup(func() { _ = srv.Close(); receive(t, peerDone, "peer reader exit") })
			receive(t, ready, "Socket.IO CONNECT reply")
			cl.Emit("echo", "before or during upgrade")
			require.Equal(t, "before or during upgrade", receive(t, echoed, "echo"))
			if name == "default" {
				deadline := time.Now().Add(5 * time.Second)
				for server.Transport() != "websocket" {
					if time.Now().After(deadline) {
						t.Fatal("both endpoints did not finish the upgrade")
					}
					runtime.Gosched()
				}
			} else {
				require.Equal(t, name, server.Transport())
			}
			want := "polling"
			if name == "websocket" {
				want = name
			}
			require.Equal(t, want, receive(t, handshake, "initial handshake transport"))
			cl.Emit("echo", "after upgrade")
			require.Equal(t, "after upgrade", receive(t, echoed, "later echo"))
		})
	}
}
