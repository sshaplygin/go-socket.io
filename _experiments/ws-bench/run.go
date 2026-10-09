package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type config struct {
	Backend       string        `json:"backend"`
	Mode          string        `json:"mode"`
	Connections   int           `json:"connections"`
	Messages      int           `json:"messages_per_connection"`
	Size          int           `json:"payload_bytes"`
	Timeout       time.Duration `json:"timeout_ns"`
	ServerCommand []string      `json:"-"`
}

func (c config) validate() error {
	if c.Backend != "gorilla" && c.Backend != "gobwas-prototype" {
		return fmt.Errorf("unknown backend %q", c.Backend)
	}
	if c.Mode != "echo" && c.Mode != "idle" {
		return fmt.Errorf("unknown mode %q", c.Mode)
	}
	if c.Connections < 1 || c.Connections > 100000 || c.Messages < 1 || c.Size < 0 || c.Size > messageLimit || c.Timeout <= 0 {
		return fmt.Errorf("invalid limits: connections 1..100000, messages > 0, size 0..%d, timeout > 0", messageLimit)
	}
	if c.Mode == "echo" && c.Messages > 10000000/c.Connections {
		return fmt.Errorf("at most 10000000 latency samples per run")
	}
	return nil
}

type result struct {
	Config               config   `json:"config"`
	GoVersion            string   `json:"go_version"`
	OS                   string   `json:"os"`
	Arch                 string   `json:"arch"`
	CPUCount             int      `json:"logical_cpus"`
	GOMAXPROCS           int      `json:"gomaxprocs"`
	Before               snapshot `json:"server_before"`
	After                snapshot `json:"server_after"`
	Echoes               int      `json:"completed_echoes"`
	ElapsedNS            int64    `json:"elapsed_ns"`
	EchoesPerSecond      float64  `json:"echoes_per_second"`
	PayloadMiBPerSecond  float64  `json:"round_trip_payload_mib_per_second"`
	P50NS                int64    `json:"rtt_p50_ns"`
	P95NS                int64    `json:"rtt_p95_ns"`
	P99NS                int64    `json:"rtt_p99_ns"`
	ServerBytesPerEcho   float64  `json:"server_interval_allocated_bytes_per_echo"`
	ServerMallocsPerEcho float64  `json:"server_interval_mallocs_per_echo"`
	HeapDeltaPerConn     float64  `json:"server_idle_heap_alloc_delta_per_connection"`
	GoroutineDelta       int      `json:"server_idle_goroutine_delta"`
}

func startServer(ctx context.Context, c config) (string, func(), error) {
	if len(c.ServerCommand) == 0 {
		return "", nil, fmt.Errorf("missing subprocess command")
	}
	args := append(append([]string{}, c.ServerCommand[1:]...), "-serve", "-backend", c.Backend, "-timeout", c.Timeout.String())
	cmd := exec.CommandContext(ctx, c.ServerCommand[0], args...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", nil, err
	}
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}
	var once sync.Once
	cleanup := func() { once.Do(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }) }
	address, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("server startup: %w", err)
	}
	return strings.TrimSpace(address), cleanup, nil
}

func getStats(ctx context.Context, client *http.Client, address string, gc bool) (snapshot, error) {
	var s snapshot
	url := "http://" + address + "/stats"
	if gc {
		url += "?gc=1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return s, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return s, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return s, fmt.Errorf("stats: HTTP %d", resp.StatusCode)
	}
	err = json.NewDecoder(resp.Body).Decode(&s)
	return s, err
}

func run(ctx context.Context, c config) (r result, err error) {
	if err := c.validate(); err != nil {
		return r, err
	}
	// Also bound callers that have not provided a deadline.
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	address, cleanup, err := startServer(ctx, c)
	if err != nil {
		return r, err
	}
	defer cleanup()
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	// Warm the stats HTTP connection/JSON path before the baseline.
	if _, err := getStats(ctx, client, address, true); err != nil {
		return r, err
	}
	r = result{Config: c, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, CPUCount: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0)}
	if r.Before, err = getStats(ctx, client, address, true); err != nil {
		return r, err
	}
	conns := make([]*websocket.Conn, 0, c.Connections)
	defer func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()
	dialer := websocket.Dialer{ReadBufferSize: 4096, WriteBufferSize: 4096, EnableCompression: false, HandshakeTimeout: 5 * time.Second, Proxy: nil}
	deadline, _ := ctx.Deadline()
	payload := bytes.Repeat([]byte{'x'}, c.Size)
	for i := 0; i < c.Connections; i++ {
		conn, response, e := dialer.DialContext(ctx, "ws://"+address+"/echo", nil)
		if e != nil {
			if response != nil {
				_ = response.Body.Close()
			}
			return r, fmt.Errorf("dial %d/%d: %w", i+1, c.Connections, e)
		}
		conns = append(conns, conn)
		_ = conn.SetReadDeadline(deadline)
		_ = conn.SetWriteDeadline(deadline)
		conn.SetReadLimit(messageLimit)
		// One acknowledged warmup per connection proves the server handler is
		// active, and includes post-first-message buffers in idle measurements.
		if _, e := roundTrip(conn, payload); e != nil {
			return r, fmt.Errorf("warmup %d: %w", i, e)
		}
	}
	if c.Mode == "idle" {
		r.After, err = getStats(ctx, client, address, true)
		if err == nil && r.After.Active != int64(c.Connections) {
			err = fmt.Errorf("only %d/%d connections remain active", r.After.Active, c.Connections)
		}
		r.HeapDeltaPerConn = (float64(r.After.HeapAlloc) - float64(r.Before.HeapAlloc)) / float64(c.Connections)
		r.GoroutineDelta = r.After.Goroutines - r.Before.Goroutines
		return r, err
	}
	if r.Before, err = getStats(ctx, client, address, true); err != nil {
		return r, err
	}
	samples := make([]int64, c.Connections*c.Messages)
	var wg sync.WaitGroup
	errors := make(chan error, c.Connections)
	start := make(chan struct{})
	for i, conn := range conns {
		wg.Add(1)
		go func(i int, conn *websocket.Conn) {
			defer wg.Done()
			<-start
			for j := 0; j < c.Messages; j++ {
				elapsed, err := roundTrip(conn, payload)
				if err != nil {
					errors <- err
					cancel() // Ensure peers unblock on a failed workload.
					for _, peer := range conns {
						_ = peer.Close()
					}
					return
				}
				samples[i*c.Messages+j] = elapsed
			}
		}(i, conn)
	}
	began := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(began)
	close(errors)
	for e := range errors {
		return r, fmt.Errorf("echo workload: %w", e)
	}
	r.After, err = getStats(ctx, client, address, false)
	if err != nil {
		return r, err
	}
	if r.After.Active != int64(c.Connections) {
		return r, fmt.Errorf("connection count changed during echo workload")
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	r.Echoes = len(samples)
	r.ElapsedNS = elapsed.Nanoseconds()
	r.EchoesPerSecond = float64(r.Echoes) / elapsed.Seconds()
	r.PayloadMiBPerSecond = r.EchoesPerSecond * float64(c.Size) * 2 / (1 << 20)
	r.P50NS, r.P95NS, r.P99NS = percentile(samples, .50), percentile(samples, .95), percentile(samples, .99)
	r.ServerBytesPerEcho = float64(r.After.TotalAlloc-r.Before.TotalAlloc) / float64(r.Echoes)
	r.ServerMallocsPerEcho = float64(r.After.Mallocs-r.Before.Mallocs) / float64(r.Echoes)
	return r, nil
}

func roundTrip(conn *websocket.Conn, payload []byte) (int64, error) {
	started := time.Now()
	if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		return 0, err
	}
	op, received, err := conn.ReadMessage()
	elapsed := time.Since(started).Nanoseconds()
	if err != nil {
		return 0, err
	}
	if op != websocket.BinaryMessage || !bytes.Equal(received, payload) {
		return 0, fmt.Errorf("echo mismatch: opcode %d, bytes %d", op, len(received))
	}
	return elapsed, nil
}

// percentile uses the nearest-rank definition on sorted, nonempty samples.
func percentile(sorted []int64, p float64) int64 {
	return sorted[int(math.Ceil(float64(len(sorted))*p))-1]
}
