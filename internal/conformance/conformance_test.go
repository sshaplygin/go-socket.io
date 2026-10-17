//go:build conformance

package conformance

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/parser"
)

// nodeEnvironment is called by tests, not TestMain: the parity gate must be able
// to list tagged tests without installing Node or running a scenario.
func nodeEnvironment(t *testing.T) (string, []string) {
	t.Helper()
	modules := os.Getenv("CONFORMANCE_NODE_MODULES")
	if !filepath.IsAbs(modules) {
		t.Fatal("CONFORMANCE_NODE_MODULES must name the absolute client node_modules directory")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("conformance requires node: %v", err)
	}
	env := append(os.Environ(), "NODE_PATH="+modules)
	return node, env
}

func runNode(t *testing.T, script string, args ...string) {
	t.Helper()
	node, env := nodeEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, append([]string{script}, args...)...)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node scenario failed: %v\n%s", err, output)
	}
	t.Logf("%s", output)
}

func socketServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := socketio.NewServer(nil)
	for _, name := range []string{"/", "/chat"} {
		n := s.Of(name)
		n.OnConnect(func(socketio.Conn) error { return nil })
		n.OnEvent("echo", func(_ socketio.Conn, value string) string { return value })
		n.OnEvent("id", func(c socketio.Conn) string { return c.ID() })
		n.OnEvent("binary", func(c socketio.Conn, value *parser.Buffer) *parser.Buffer {
			c.Emit("binary-event", value)
			return value
		})
		n.OnEvent("ask", func(c socketio.Conn) {
			c.Emit("question", "server", func(value string) { c.Emit("ack-result", value) })
		})
		n.OnEvent("ask-binary", func(c socketio.Conn) {
			c.Emit("binary-question", &parser.Buffer{Data: []byte{0, 255, 2}}, func(value *parser.Buffer) {
				c.Emit("binary-ack-result", value)
			})
		})
		n.OnEvent("join", func(c socketio.Conn, room string) string { c.Join(room); return "joined" })
		n.OnEvent("broadcast", func(c socketio.Conn, room string) string {
			if err := c.To(room).Emit("news", "room"); err != nil {
				return err.Error()
			}
			_ = n.Emit("barrier")
			return "sent"
		})
		n.OnEvent("disconnect-me", func(c socketio.Conn) { c.Disconnect() })
		n.OnEvent("drop-transport", func(c socketio.Conn) { _ = c.EngineConn().Close() })
	}
	s.Of("/denied").Use(func(socketio.Conn) error { return errors.New("access denied") })
	served := make(chan struct{})
	go func() { _ = s.Serve(); close(served) }()
	ts := httptest.NewServer(s)
	t.Cleanup(func() {
		_ = s.Close()
		ts.Close()
		<-served
	})
	return ts
}

func TestJSConformance(t *testing.T) {
	for _, transport := range []string{"polling", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			ts := socketServer(t)
			runNode(t, "testdata/node/client.js", ts.URL, transport)
		})
	}
}

func TestJSUpgrade(t *testing.T) {
	ts := socketServer(t)
	runNode(t, "testdata/node/client.js", ts.URL, "upgrade")
}
