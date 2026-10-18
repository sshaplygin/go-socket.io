//go:build conformance

package conformance

import (
	"bufio"
	"context"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// Covers E8
func TestConformancePerMessageDeflate(t *testing.T) {
	s := socketio.NewServer(&socketio.Options{Engine: &engineio.Options{PerMessageDeflate: true}})
	s.OnConnect("/", func(socketio.Conn) error { return nil })
	s.OnEvent("/", "echo", func(c socketio.Conn, value string) string { c.Emit("reply", value); return value })
	served := make(chan struct{})
	go func() { _ = s.Serve(); close(served) }()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close(); <-served })
	runNode(t, "testdata/node/compression-client.js", ts.URL)
}

// Covers C11
func TestConformanceClientPerMessageDeflate(t *testing.T) {
	node, env := nodeEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, node, "testdata/node/server/compression-server.js")
	cmd.Env = env
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "http://127.0.0.1:") {
		t.Fatalf("compression reference did not start: %s", stderr.String())
	}
	cl, err := client.New(scanner.Text(), &client.Options{Transports: []transport.Transport{websocket.Default}, PerMessageDeflate: true, DisableReconnection: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	connected := make(chan client.Conn, 1)
	reply := make(chan string, 1)
	cl.OnConnect(func(c client.Conn) error { connected <- c; return nil })
	cl.OnEvent("reply", func(_ client.Conn, v string) { reply <- v })
	require.NoError(t, cl.Connect())
	c := goClientReceive(t, connected, "compressed namespace")
	type result struct{ Value, Extension string }
	ack := make(chan result, 1)
	value := strings.Repeat("compression", 400)
	require.NoError(t, c.Compress(true).Emit("echo", value, func(r result) { ack <- r }))
	require.Equal(t, value, goClientReceive(t, reply, "4KB compressed event"))
	got := goClientReceive(t, ack, "4KB compressed ACK")
	require.Equal(t, value, got.Value)
	require.Contains(t, got.Extension, "permessage-deflate")
}
