//go:build conformance

package conformance

import (
	"bufio"
	"context"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/sshaplygin/go-socket.io/parser"
)

func goClientReference(t *testing.T) string {
	t.Helper()
	node, env := nodeEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	cmd := exec.CommandContext(ctx, node, "testdata/node/server/goclient-server.js")
	cmd.Env = env
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "http://127.0.0.1:") {
		cancel()
		_ = cmd.Wait()
		t.Fatalf("Go-client reference did not start: %s", stderr.String())
	}
	return scanner.Text()
}

func goClientReceive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for %s", what)
		var zero T
		return zero
	}
}

type goClientMetadata struct {
	SID, ID, Namespace, Transport, Query, NamespaceQuery, Header, URL string
	Connected                                                         []string
	Disconnected                                                      [][]string
	Received                                                          [][]string
}

func TestGoClientConformance(t *testing.T) {
	for _, name := range []string{"polling", "websocket", "upgrade"} {
		t.Run(name, func(t *testing.T) {
			opts := []client.Option{client.WithPath("/client-engine"), client.WithQuery(url.Values{"custom": {"value"}}), client.WithHeader(http.Header{"X-Go-Client": {"header"}})}
			if name == "polling" {
				opts = append(opts, client.WithTransports(polling.Default))
			}
			if name == "websocket" {
				opts = append(opts, client.WithTransports(websocket.Default))
			}
			cl, err := client.New(goClientReference(t), opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			connected := make(chan client.Conn, 4)
			replies, answers := make(chan string, 4), make(chan string, 4)
			install := func(s *client.Socket) {
				s.OnConnect(func(c client.Conn) error { connected <- c; return nil })
				s.OnEvent("reply", func(_ client.Conn, value string) { replies <- value })
				s.OnEvent("question", func(_ client.Conn, value string) string { return value + ":client" })
				s.OnEvent("answer", func(_ client.Conn, value string) { answers <- value })
			}
			root, chat := cl.Socket("/"), cl.Socket("/chat?namespace=chat")
			install(root)
			install(chat)
			require.NoError(t, cl.Connect())
			conns := make(map[string]client.Conn)
			for i := 0; i < 2; i++ {
				c := goClientReceive(t, connected, "initial CONNECT")
				require.NotContains(t, conns, c.Namespace())
				conns[c.Namespace()] = c
			}
			late := cl.Socket("/late")
			install(late)
			c := goClientReceive(t, connected, "late namespace CONNECT")
			require.Equal(t, "/late", c.Namespace())
			conns[c.Namespace()] = c
			inspect := func(s *client.Socket) goClientMetadata {
				result := make(chan goClientMetadata, 1)
				s.Emit("inspect", func(value goClientMetadata) { result <- value })
				return goClientReceive(t, result, "Node metadata ACK")
			}
			if name == "upgrade" {
				upgraded := make(chan string, 1)
				root.Emit("await-upgrade", func(value string) { upgraded <- value })
				require.Equal(t, "websocket", goClientReceive(t, upgraded, "Node upgrade"))
			}
			for _, s := range []*client.Socket{root, chat, late} {
				meta := inspect(s)
				require.Equal(t, conns["/"].ID(), meta.SID)
				require.Equal(t, conns[s.Namespace()].ID(), meta.ID)
				require.Equal(t, s.Namespace(), meta.Namespace)
				wantTransport := name
				if name == "upgrade" {
					wantTransport = "websocket"
				}
				require.Equal(t, wantTransport, meta.Transport)
				require.Equal(t, "value", meta.Query)
				require.Equal(t, "header", meta.Header)
				require.True(t, strings.HasPrefix(meta.URL, "/client-engine/"))
				if s == chat {
					require.Equal(t, "chat", meta.NamespaceQuery)
				}
				ack := make(chan string, 1)
				s.Emit("echo", "Привет 世界", func(value string) { ack <- value })
				require.Equal(t, "Привет 世界", goClientReceive(t, replies, "Node event"))
				require.Equal(t, "Привет 世界", goClientReceive(t, ack, "Node ACK"))
				s.Emit("ask")
				require.Equal(t, "server:client", goClientReceive(t, answers, "Go ACK to Node event"))
				binary := make(chan *parser.Buffer, 1)
				s.Emit("binary", &parser.Buffer{Data: []byte{0, 255, 2}}, func(value *parser.Buffer) { binary <- value })
				require.Equal(t, []byte{0, 255, 2}, goClientReceive(t, binary, "Node binary ACK").Data)
			}
			require.NoError(t, chat.Close())
			meta := inspect(root)
			require.ElementsMatch(t, []string{"/", "/chat", "/late"}, meta.Connected)
			require.Equal(t, [][]string{{"/chat", "client namespace disconnect"}}, meta.Disconnected)
			require.Empty(t, connected, "each namespace connects once")
		})
	}
}

// socket.io 2.5.0 joins a namespace again on the same Engine.IO connection when the
// client sends CONNECT after the server's socket.disconnect() or the client's
// DISCONNECT. It joins the root namespace only when a connection opens, and holds a
// namespace CONNECT that arrives while the root namespace is disconnected.
func TestGoClientNamespaceRejoin(t *testing.T) {
	for _, name := range []string{"polling", "websocket"} {
		t.Run(name, func(t *testing.T) {
			opts := []client.Option{client.WithPath("/client-engine"), client.WithTransports(polling.Default)}
			if name == "websocket" {
				opts[1] = client.WithTransports(websocket.Default)
			}
			cl, err := client.New(goClientReference(t), opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			connected, disconnected := make(chan string, 8), make(chan string, 8)
			root, chat, late := cl.Socket("/"), cl.Socket("/chat"), cl.Socket("/late")
			for _, s := range []*client.Socket{root, chat, late} {
				s.OnConnect(func(c client.Conn) error { connected <- c.Namespace(); return nil })
				s.OnDisconnect(func(c client.Conn, reason string) { disconnected <- c.Namespace() + ":" + reason })
			}
			require.NoError(t, cl.Connect())
			for i := 0; i < 3; i++ {
				goClientReceive(t, connected, "initial CONNECT")
			}
			inspect := func(s *client.Socket) goClientMetadata {
				result := make(chan goClientMetadata, 1)
				s.Emit("inspect", func(value goClientMetadata) { result <- value })
				return goClientReceive(t, result, "Node metadata ACK")
			}
			echo := func(s *client.Socket, value string) {
				ack := make(chan string, 1)
				s.Emit("echo", value, func(v string) { ack <- v })
				require.Equal(t, value, goClientReceive(t, ack, "Node ACK"))
			}
			sid := inspect(root).SID

			chat.Emit("kick")
			require.Equal(t, "/chat:io server disconnect", goClientReceive(t, disconnected, "server namespace disconnect"))
			buffered := make(chan string, 1)
			chat.Emit("echo", "while closed", func(v string) { buffered <- v })
			require.NoError(t, chat.Connect())
			require.Equal(t, "/chat", goClientReceive(t, connected, "CONNECT after Socket.Connect"))
			require.Equal(t, "while closed", goClientReceive(t, buffered, "ACK of the buffered event"))
			echo(chat, "after server disconnect")

			require.NoError(t, chat.Close())
			require.Equal(t, "/chat:io client disconnect", goClientReceive(t, disconnected, "Socket.Close"))
			require.NoError(t, chat.Connect())
			require.Equal(t, "/chat", goClientReceive(t, connected, "CONNECT after Close and Connect"))
			echo(chat, "after client disconnect")
			meta := inspect(root)
			require.Equal(t, sid, meta.SID, "the namespace rejoined the same Engine.IO connection")
			require.ElementsMatch(t, []string{"/", "/chat", "/late", "/chat", "/chat"}, meta.Connected)
			require.Equal(t, [][]string{{"/chat", "server namespace disconnect"}, {"/chat", "client namespace disconnect"}}, meta.Disconnected)

			root.Emit("kick")
			require.Equal(t, "/:io server disconnect", goClientReceive(t, disconnected, "root disconnect"))
			require.ErrorIs(t, root.Connect(), client.ErrRootNamespaceRejoin)
			require.NoError(t, chat.Close())
			require.Equal(t, "/chat:io client disconnect", goClientReceive(t, disconnected, "Socket.Close"))
			require.NoError(t, chat.Connect())
			meta = inspect(late)
			require.Equal(t, sid, meta.SID)
			require.ElementsMatch(t, []string{"/", "/chat", "/late", "/chat", "/chat"}, meta.Connected, "the CONNECT waits for a root namespace")
			require.False(t, chat.Connected())
			require.Empty(t, connected, "each join calls OnConnect once")
		})
	}
}

// A socket.disconnect() that crosses the client's Close and Connect leaves the
// namespace joined on both sides: the Go client takes the server's DISCONNECT as
// the end of the namespace socket that Close ended.
func TestGoClientCrossingDisconnect(t *testing.T) {
	for _, name := range []string{"polling", "websocket"} {
		t.Run(name, func(t *testing.T) {
			opts := []client.Option{client.WithPath("/client-engine"), client.WithTransports(polling.Default)}
			if name == "websocket" {
				opts[1] = client.WithTransports(websocket.Default)
			}
			cl, err := client.New(goClientReference(t), opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			connected, disconnected := make(chan string, 8), make(chan string, 8)
			root, chat := cl.Socket("/"), cl.Socket("/chat")
			for _, s := range []*client.Socket{root, chat} {
				s.OnConnect(func(c client.Conn) error { connected <- c.Namespace(); return nil })
				s.OnDisconnect(func(c client.Conn, reason string) { disconnected <- c.Namespace() + ":" + reason })
			}
			require.NoError(t, cl.Connect())
			goClientReceive(t, connected, "initial CONNECT")
			goClientReceive(t, connected, "initial CONNECT")

			chat.Emit("kick")
			require.NoError(t, chat.Close())
			require.NoError(t, chat.Connect())
			require.Equal(t, "/chat", goClientReceive(t, connected, "CONNECT after Socket.Connect"))
			ack := make(chan string, 1)
			chat.Emit("echo", "rejoined", func(v string) { ack <- v })
			require.Equal(t, "rejoined", goClientReceive(t, ack, "Node ACK"))
			require.True(t, chat.Connected())
			// Whether the kick crosses Close depends on timing; the result does not.
			require.Contains(t, []string{"/chat:io client disconnect", "/chat:io server disconnect"}, goClientReceive(t, disconnected, "chat disconnect"))
			result := make(chan goClientMetadata, 1)
			chat.Emit("inspect", func(value goClientMetadata) { result <- value })
			meta := goClientReceive(t, result, "Node metadata ACK")
			require.Equal(t, []string{"/", "/chat", "/chat"}, meta.Connected)
			require.Len(t, meta.Disconnected, 1, "Node keeps the rejoined /chat socket")
			require.Empty(t, connected, "OnConnect once per join")
			require.Empty(t, disconnected)
		})
	}
}

func TestGoClientReconnect(t *testing.T) {
	for _, name := range []string{"polling", "websocket", "upgrade"} {
		t.Run(name, func(t *testing.T) {
			opts := []client.Option{client.WithPath("/client-engine"), client.WithReconnectionDelay(10 * time.Millisecond), client.WithReconnectionDelayMax(20 * time.Millisecond), client.WithRandomizationFactor(-1), client.WithReconnectionAttempts(3)}
			if name == "polling" {
				opts = append(opts, client.WithTransports(polling.Default))
			}
			if name == "websocket" {
				opts = append(opts, client.WithTransports(websocket.Default))
			}
			cl, err := client.New(goClientReference(t), opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			connected := make(chan client.Conn, 6)
			disconnected := make(chan string, 4)
			reconnected := make(chan int, 1)
			reentrant := make(chan error, 1)
			for _, s := range []*client.Socket{cl.Socket("/"), cl.Socket("/chat")} {
				s.OnConnect(func(c client.Conn) error { connected <- c; return nil })
				s.OnDisconnect(func(c client.Conn, reason string) { disconnected <- c.Namespace() + ":" + reason })
			}
			cl.OnReconnect(func(n int) { reentrant <- cl.Connect(); reconnected <- n })
			require.NoError(t, cl.Connect())
			old := map[string]string{}
			for i := 0; i < 2; i++ {
				c := goClientReceive(t, connected, "initial Node namespace")
				old[c.Namespace()] = c.ID()
			}
			if name == "upgrade" {
				upgraded := make(chan string, 1)
				cl.Emit("await-upgrade", func(v string) { upgraded <- v })
				require.Equal(t, "websocket", goClientReceive(t, upgraded, "upgrade before failure"))
			}
			cl.Emit("drop-transport")
			for i := 0; i < 2; i++ {
				reason := goClientReceive(t, disconnected, "Node transport disconnect")
				// Node closes a websocket with a close frame and a polling session with CLOSE.
				require.Equal(t, "transport close", strings.SplitN(reason, ":", 2)[1])
			}
			require.Equal(t, 1, goClientReceive(t, reconnected, "Node reconnect"))
			require.NoError(t, goClientReceive(t, reentrant, "Connect inside OnReconnect"))
			for i := 0; i < 2; i++ {
				c := goClientReceive(t, connected, "restored Node namespace")
				require.True(t, c.Connected())
				require.NotEqual(t, old[c.Namespace()], c.ID())
				ack := make(chan string, 1)
				c.Emit("echo", "after reconnect", func(v string) { ack <- v })
				require.Equal(t, "after reconnect", goClientReceive(t, ack, "restored ACK"))
			}
			require.Empty(t, connected)
		})
	}
}
