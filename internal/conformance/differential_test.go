//go:build conformance

package conformance

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/parser"
)

func differentialGoServer(t *testing.T, configure ...func(*socketio.Server)) *httptest.Server {
	t.Helper()
	s := socketio.NewServer()
	for _, name := range []string{"/", "/chat"} {
		n := s.Of(name)
		n.OnConnect(func(socketio.Conn) error { return nil })
		n.OnEvent("echo", func(_ socketio.Conn, value string) string { return value })
		n.OnEvent("identity", func(c socketio.Conn) string { return c.ID() })
		n.OnEvent("metadata", differentialMetadata)
		n.OnEvent("ask", func(c socketio.Conn) {
			c.Emit("question", "server", func(value string) { c.Emit("ack-result", value) })
		})
		n.OnEvent("binary", func(c socketio.Conn, value *parser.Buffer) *parser.Buffer {
			c.Emit("binary-event", value)
			return value
		})
		n.OnEvent("ask-binary", func(c socketio.Conn) {
			c.Emit("binary-question", &parser.Buffer{Data: []byte{0, 255, 2}}, func(value *parser.Buffer) {
				c.Emit("binary-result", value)
			})
		})
		n.OnEvent("join", func(c socketio.Conn, room string) string { c.Join(room); return "joined" })
		n.OnEvent("leave", func(c socketio.Conn, room string) string { c.Leave(room); return "left" })
		n.OnEvent("room", func(c socketio.Conn, room string, except bool) string {
			if except {
				_ = c.To(room).Emit("news", "others")
			} else {
				_ = n.To(room).Emit("news", "all")
			}
			_ = n.Emit("barrier")
			return "sent"
		})
		n.OnEvent("disconnect-one", func(c socketio.Conn) { c.Disconnect() })
		n.OnEvent("disconnect-all", func(c socketio.Conn) { _ = c.Close() })
		n.OnEvent("drop-transport", func(c socketio.Conn) { _ = c.EngineConn().Close() })
	}
	s.Of("/denied").Use(func(socketio.Conn) error { return errors.New("access denied") })
	for _, apply := range configure {
		apply(s)
	}
	served := make(chan struct{})
	go func() { _ = s.Serve(); close(served) }()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close(); <-served })
	return ts
}

func differentialMetadata(c socketio.Conn) map[string]interface{} {
	h := c.Handshake()
	return map[string]interface{}{"query": h.Query.Get("custom"), "header": h.Headers.Get("X-Differential"),
		"time": h.Time.Format(time.RFC3339Nano), "issued": h.Time.UnixMilli()}
}

func differentialReference(t *testing.T) string {
	t.Helper()
	node, env := nodeEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	cmd := exec.CommandContext(ctx, node, "testdata/node/server/reference-server.js")
	cmd.Env = env
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	scanner := bufio.NewScanner(out)
	if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "http://127.0.0.1:") {
		cancel()
		_ = cmd.Wait()
		t.Fatalf("reference server did not become ready: %s", stderr.String())
	}
	return scanner.Text()
}

func differentialTranscript(t *testing.T, url, scenario, transport string) []interface{} {
	t.Helper()
	node, env := nodeEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "testdata/node/differential-client.js", url, scenario, transport)
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("differential client %s %s: %v\n%s\n%s", scenario, transport, err, stderr.String(), output)
	}
	var transcript []interface{}
	if err := json.Unmarshal(output, &transcript); err != nil {
		t.Fatalf("invalid transcript: %v\n%s", err, output)
	}
	normalizeDifferential(t, transcript)
	return transcript
}

// Only dynamic identifiers and parseable handshake timestamps are normalized.
// The fixture has already sorted records by socket and sequence, leaving every
// event's per-socket order and arguments available for exact comparison.
func normalizeDifferential(t *testing.T, transcript []interface{}) {
	t.Helper()
	ids := make(map[string]string)
	engines := make(map[string]string)
	for _, raw := range transcript {
		record := raw.(map[string]interface{})
		args := record["args"].([]interface{})
		switch record["event"] {
		case "connect", "open":
			value := args[0].(map[string]interface{})
			sid, ok := value["sid"].(string)
			if !ok || sid == "" {
				t.Fatal("missing session ID in transcript")
			}
			if engines[sid] == "" {
				engines[sid] = fmt.Sprintf("connection-%d", len(engines))
			}
			ids[sid] = engines[sid]
			if record["event"] == "connect" {
				namespace := value["namespace"].(string)
				id, ok := value["id"].(string)
				// The 1.7 client exposes the bare engine ID even outside root;
				// 2.5 adds the namespace prefix. Preserve whichever it observed.
				prefix := strings.TrimSuffix(id, sid)
				if !ok || !strings.HasSuffix(id, sid) || prefix != "" && prefix != namespace+"#" {
					t.Fatalf("unexpected socket ID shape: %#v", value)
				}
				ids[id] = prefix + engines[sid]
				if namespace != "/" {
					// Server-reported IDs keep the namespace prefix with either client.
					ids[namespace+"#"+sid] = namespace + "#" + engines[sid]
				}
			}
		case "ack:metadata":
			value := args[0].(map[string]interface{})
			text, ok := value["time"].(string)
			if _, err := time.Parse(time.RFC3339Nano, text); !ok || err != nil {
				t.Fatalf("invalid handshake time: %#v", value)
			}
			issued, ok := value["issued"].(float64)
			if !ok || issued <= 0 {
				t.Fatalf("invalid handshake issued: %#v", value)
			}
			value["time"], value["issued"] = "parseable", "parseable"
		}
	}
	var replace func(interface{}) interface{}
	replace = func(value interface{}) interface{} {
		switch v := value.(type) {
		case string:
			if normalized, ok := ids[v]; ok {
				return normalized
			}
		case []interface{}:
			for i := range v {
				v[i] = replace(v[i])
			}
		case map[string]interface{}:
			for key := range v {
				v[key] = replace(v[key])
			}
		}
		return value
	}
	replace(transcript)
}

func TestDifferential(t *testing.T) {
	for _, scenario := range []string{"connect-root", "namespace", "middleware-reject", "ack-both-ways", "binary", "non-ascii", "rooms", "server-disconnect", "handshake-data", "rejected-handshake", "cors", "open-packet"} {
		for _, transport := range []string{"polling", "websocket"} {
			t.Run(scenario+"-"+transport, func(t *testing.T) {
				goServer := differentialGoServer(t)
				reference := differentialReference(t)
				want := differentialTranscript(t, reference, scenario, transport)
				got := differentialTranscript(t, goServer.URL, scenario, transport)
				if reflect.DeepEqual(got, want) {
					return
				}
				for i := 0; i < len(got) || i < len(want); i++ {
					if i >= len(got) || i >= len(want) || !reflect.DeepEqual(got[i], want[i]) {
						t.Fatalf("first transcript difference at record %d\nGo: %s\nNode: %s", i, differentialJSON(got), differentialJSON(want))
					}
				}
			})
		}
	}
}

// These adversarial handlers differ only by an extra ACK argument. The same
// executable fixture used by TestDifferential must retain that difference.
func TestDifferentialExtraACKArguments(t *testing.T) {
	for _, scenario := range []string{"binary", "handshake-data"} {
		for _, transport := range []string{"polling", "websocket"} {
			t.Run(scenario+"-"+transport, func(t *testing.T) {
				bad := differentialGoServer(t, func(s *socketio.Server) {
					s.OnEvent("/", "binary", func(c socketio.Conn, value *parser.Buffer) (*parser.Buffer, string) {
						c.Emit("binary-event", value)
						return value, "unexpected extra ACK argument"
					})
					s.OnEvent("/", "metadata", func(c socketio.Conn) (map[string]interface{}, string) {
						return differentialMetadata(c), "unexpected extra ACK argument"
					})
				})
				reference := differentialReference(t)
				want := differentialTranscript(t, reference, scenario, transport)
				got := differentialTranscript(t, bad.URL, scenario, transport)
				if reflect.DeepEqual(got, want) {
					t.Fatal("differential fixture discarded an extra ACK argument")
				}
			})
		}
	}
}

func differentialJSON(value interface{}) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}
