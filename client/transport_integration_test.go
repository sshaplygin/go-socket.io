package client

import (
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/stretchr/testify/require"
)

// Covers C5
func TestClientWebsocketUpgrade(t *testing.T) {
	for _, name := range []string{"default", "polling", "websocket"} {
		t.Run(name, func(t *testing.T) {
			srv := engineio.NewServer(nil)
			handshake := make(chan string, 1)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("sid") == "" {
					handshake <- r.URL.Query().Get("transport")
				}
				srv.ServeHTTP(w, r)
			}))
			var opts Options
			if name == "polling" {
				opts.Transports = []transport.Transport{polling.Default}
			}
			if name == "websocket" {
				opts.Transports = []transport.Transport{websocket.Default}
			}
			cl, err := New(ts.URL, &opts)
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
				writer, err := server.NextWriter(session.TEXT)
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
			t.Cleanup(func() { _ = srv.Close(); recv(t, peerDone, "peer reader exit") })
			recv(t, ready, "Socket.IO CONNECT reply")
			cl.Emit("echo", "before or during upgrade")
			require.Equal(t, "before or during upgrade", recv(t, echoed, "echo"))
			if name == "default" {
				deadline := time.Now().Add(5 * time.Second)
				for !cl.conn.Conn.Upgraded() || server.Transport() != "websocket" {
					if time.Now().After(deadline) {
						t.Fatal("both endpoints did not finish the upgrade")
					}
					runtime.Gosched()
				}
			} else {
				require.Equal(t, name, server.Transport())
				require.Equal(t, name, cl.conn.Conn.Transport())
				require.False(t, cl.conn.Conn.Upgraded())
			}
			want := "polling"
			if name == "websocket" {
				want = name
			}
			require.Equal(t, want, recv(t, handshake, "initial handshake transport"))
			cl.Emit("echo", "after upgrade")
			require.Equal(t, "after upgrade", recv(t, echoed, "later echo"))
		})
	}
}

// A timed-out Dial can still return: its connection must be closed rather than attached.
// Covers C4
func TestClientTimeoutClosesLateConnection(t *testing.T) {
	cl, err := New("http://unused/", &Options{Timeout: 20 * time.Millisecond})
	require.NoError(t, err)
	late := newFakeConn(t)
	release, entered := make(chan struct{}), make(chan struct{})
	cl.dial = func(string) (engineio.Conn, error) { close(entered); <-release; return late, nil }
	require.ErrorIs(t, cl.Connect(), ErrConnectTimeout)
	recv(t, entered, "pending dial")
	close(release)
	recv(t, late.closed, "late connection discarded")
	require.Nil(t, cl.conn)
	require.NoError(t, cl.Close())
}

// A full application queue cannot consume the slot needed for a namespace DISCONNECT.
// Covers C10
func TestClientDisconnectWithFullQueue(t *testing.T) {
	p := start(t, 'C', hooks{}, "/chat")
	p.stall(t, p.nc, defaultWriteBufferSize)
	require.NoError(t, p.cl.Socket("/chat").Close())
	require.Equal(t, "/chat", recv(t, p.discs, "namespace close callback"))
	require.NoError(t, p.cl.Close())
	close(p.fc.release)
	require.Equal(t, ev("first"), recv(t, p.fc.out, "in-flight packet"))
	for i := 0; i < defaultWriteBufferSize; i++ {
		require.Equal(t, ev("x", i), recv(t, p.fc.out, "queued event"))
	}
	require.Equal(t, "1/chat,", recv(t, p.fc.out, "namespace DISCONNECT after earlier events"))
	require.Equal(t, "1", recv(t, p.fc.out, "root DISCONNECT"))
	recv(t, p.fc.closed, "engine closed after controls drain")
	require.Empty(t, p.errs)
	require.Empty(t, p.fc.out)
}
