package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The test executable doubles as the child server, including under -race.
func TestServerProcess(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--" {
			if err := command(os.Args[i+1:]); err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
}

func testConfig() config {
	return config{Backend: "gorilla", Mode: "echo", Connections: 2, Messages: 5, Size: 1024, Timeout: 15 * time.Second, ServerCommand: []string{os.Args[0], "-test.run=^TestServerProcess$", "--"}}
}

func TestEchoAndIdle(t *testing.T) {
	for _, backend := range []string{"gorilla", "gobwas-prototype"} {
		for _, mode := range []string{"echo", "idle"} {
			t.Run(backend+"/"+mode, func(t *testing.T) {
				c := testConfig()
				c.Backend, c.Mode = backend, mode
				r, err := run(context.Background(), c)
				if err != nil {
					t.Fatal(err)
				}
				if r.After.Active != int64(c.Connections) {
					t.Fatalf("active: %+v", r)
				}
				if mode == "echo" && (r.Echoes != c.Connections*c.Messages || r.EchoesPerSecond <= 0 || r.P50NS <= 0 || r.P50NS > r.P95NS || r.P95NS > r.P99NS || r.ServerBytesPerEcho <= 0) {
					t.Fatalf("invalid echo metrics: %+v", r)
				}
				if mode == "idle" && (r.Echoes != 0 || r.HeapDeltaPerConn <= 0 || r.GoroutineDelta < c.Connections) {
					t.Fatalf("invalid idle metrics: %+v", r)
				}
				if _, err := json.Marshal(r); err != nil {
					t.Fatalf("JSON: %v", err)
				}
			})
		}
	}
}

func TestPayloadBoundaries(t *testing.T) {
	for _, backend := range []string{"gorilla", "gobwas-prototype"} {
		for _, size := range []int{0, messageLimit} {
			c := testConfig()
			c.Backend, c.Size, c.Connections, c.Messages = backend, size, 1, 1
			if _, err := run(context.Background(), c); err != nil {
				t.Fatalf("%s size %d: %v", backend, size, err)
			}
		}
	}
}

func TestCleanupAndStartupFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	address, cleanup, err := startServer(ctx, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	cleanup() // Idempotent; Wait has reaped the subprocess before returning.
	if conn, err := net.DialTimeout("tcp", address, time.Second); err == nil {
		_ = conn.Close()
		t.Fatal("server still listening after cleanup")
	}
	c := testConfig()
	c.ServerCommand = []string{"/does-not-exist/ws-bench"}
	if _, err := run(ctx, c); err == nil {
		t.Fatal("expected startup failure")
	}
	// The test subprocess exits without a listening address.
	c.ServerCommand = []string{os.Args[0], "-test.run=^TestServerProcess$", "--", "-unknown-option"}
	if _, err := run(ctx, c); err == nil {
		t.Fatal("expected early subprocess exit")
	}
	ctx, cancelEarly := context.WithCancel(ctx)
	cancelEarly()
	if _, err := run(ctx, testConfig()); err == nil {
		t.Fatal("expected canceled workload to fail")
	}
}

func TestValidation(t *testing.T) {
	for _, change := range []func(*config){
		func(c *config) { c.Backend = "unknown" },
		func(c *config) { c.Mode = "unknown" },
		func(c *config) { c.Connections = 0 },
		func(c *config) { c.Connections = 100001 },
		func(c *config) { c.Messages = 0 },
		func(c *config) { c.Size = -1 },
		func(c *config) { c.Size = messageLimit + 1 },
		func(c *config) { c.Timeout = 0 },
		func(c *config) { c.Messages = int(^uint(0) >> 1) },
	} {
		c := testConfig()
		change(&c)
		if err := c.validate(); err == nil {
			t.Fatalf("accepted invalid config: %+v", c)
		}
	}
}

func TestPercentiles(t *testing.T) {
	samples := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	got := []int64{percentile(samples, .50), percentile(samples, .95), percentile(samples, .99), percentile([]int64{42}, .50)}
	if !reflect.DeepEqual(got, []int64{5, 10, 10, 42}) {
		t.Fatal(got)
	}
}

func TestStatsFailures(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusOK} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("invalid JSON"))
		}))
		_, err := getStats(context.Background(), server.Client(), strings.TrimPrefix(server.URL, "http://"), false)
		server.Close()
		if err == nil {
			t.Fatal("expected invalid stats response to fail")
		}
	}
}

func TestRejectsWrongEcho(t *testing.T) {
	for _, wrongType := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := websocket.Upgrader{}
			conn, err := u.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			op, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if wrongType {
				op = websocket.TextMessage
			} else {
				data[0] ^= 1 // Same length, different content.
			}
			_ = conn.WriteMessage(op, data)
		}))
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		_, err = roundTrip(conn, []byte("payload"))
		_ = conn.Close()
		server.Close()
		if err == nil {
			t.Fatal("accepted incorrect echo")
		}
	}
}

func TestCancellationStopsServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	address, cleanup, err := startServer(ctx, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	cleanup()
	if conn, err := net.DialTimeout("tcp", address, time.Second); err == nil {
		_ = conn.Close()
		t.Fatal("server survived context cancellation")
	}
}
