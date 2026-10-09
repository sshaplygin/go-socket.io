package engineio_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// BenchmarkIdleConnections opens N idle websocket Engine.IO sessions against an
// engineio.Server and reports what the server process holds for them: resident set
// size and goroutines. It is the "idle connections" baseline of ROADMAP 2.1 and is
// meant to be run again, unchanged, after the WebSocket transport is replaced.
//
// N is 200 by default so that "make bench" and the benchmark workflow (ten runs each
// for base and head) stay cheap. The roadmap figure is selected with the environment
// variable IDLE_CONNS:
//
//	IDLE_CONNS=10000 go test -run '^$' -bench BenchmarkIdleConnections -benchtime=1x ./engineio/
//
// The server runs in a subprocess (this test binary re-executed with
// IDLE_BENCH_SERVER=1, see TestIdleBenchServerProcess), so its RSS and goroutine count
// are its own; the dialing client sessions live in the benchmark process and are not
// counted. The subprocess inherits RLIMIT_NOFILE, which the benchmark raises to about
// 2*N; if the hard limit is lower, the benchmark skips with the limit in the message.
//
// Run one 10000-connection benchmark at a time, with -count=1: on macOS the TIME_WAIT
// entries of a finished run keep the ephemeral port range exhausted for about a minute
// and the next run fails to dial (the error says so).
//
// A run with more connections than the loopback ephemeral range holds (about 16000 on
// macOS by default) fails the same way.
//
// Only the public engineio.Server, client.Dialer and websocket.Default are used, so the
// same file measures any WebSocket implementation behind them. The benchmark skips
// outside linux and darwin (it needs RLIMIT_NOFILE) and where ps is missing or does not
// support -p.
//
// Every run logs one line with the server RSS before and after, B/conn, goroutines and
// the connect phase (visible in -bench output). The benchmark workflow's report tool
// rejects any benchmark metric unit other than ns/op, B/op, allocs/op and MB/s, so the
// custom metrics below are reported only when IDLE_CONNS is set, that is, in explicit
// manual runs, never in "make bench" or the workflow:
//
//	ns/op                wall time of one whole run: start the server, dial N sessions,
//	                     hold them idle for at least one second, tear down
//	connect-us/conn      connect phase wall time divided by N (32 parallel dialers)
//	rss-B/conn           (server RSS after - server RSS before) / N
//	rss-total-MiB        server RSS after N connections
//	goroutines/conn      (server goroutines after - before) / N
//	server-goroutines    server goroutines after N connections
//
// The whole run is timed and holds the sessions idle for at least one second. A run
// shorter than -benchtime would make the testing package raise b.N and start dozens of
// server subprocesses; with the hold it stays at b.N=1 for the default -benchtime.
// Before measuring, the server runs debug.FreeOSMemory, so RSS excludes garbage the Go
// runtime has not yet returned to the system. RSS comes from "ps -o rss=" on the server
// pid. The numbers describe one machine and one run; they are advisory.
//
// Limits of the measurement: a run lasts a few seconds, shorter than the default
// 20 s Options.PingInterval, so the sessions are idle without any ping/pong and the
// heartbeat cost is not measured. The server is cold when the sessions are opened, so
// one-time warm-up (runtime and HTTP server structures) is counted in RSS and weighs
// more per session the smaller N is.
//
// In a default run (IDLE_CONNS unset) the standard metrics are not the measured
// quantity: ns/op includes the one-second hold and the subprocess start-up, and B/op and
// allocs/op are those of the dialing client in this process, not of the server. They
// appear in the base-vs-PR benchmark comparison only because the testing package always
// reports them; read the log line above for the server figures.
func BenchmarkIdleConnections(b *testing.B) {
	if !idleBenchSupported {
		b.Skipf("idle benchmark needs linux or darwin, not %s", runtime.GOOS)
	}
	// A host without a usable ps (busybox, minimal containers) skips instead of failing
	// "make bench" for the whole repository.
	if _, err := readRSS(os.Getpid()); err != nil {
		b.Skipf("idle benchmark needs a working ps: %v", err)
	}
	n := idleConns(b)
	if err := raiseNoFile(uint64(2*n + 512)); err != nil {
		b.Skipf("cannot open %d connections: %v", n, err)
	}

	var last idleResult
	for i := 0; i < b.N; i++ {
		last = runIdleIteration(b, n)
	}
	if os.Getenv("IDLE_CONNS") != "" {
		b.ReportMetric(last.connectUsPerConn, "connect-us/conn")
		b.ReportMetric(last.rssPerConn, "rss-B/conn")
		b.ReportMetric(last.rssTotalMiB, "rss-total-MiB")
		b.ReportMetric(last.goroutinesPerConn, "goroutines/conn")
		b.ReportMetric(last.goroutines, "server-goroutines")
	}
}

// idleConns reads IDLE_CONNS: 1..100000, default 200.
func idleConns(b *testing.B) int {
	v := os.Getenv("IDLE_CONNS")
	if v == "" {
		return 200
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 100000 {
		b.Fatalf("IDLE_CONNS=%q: want an integer in 1..100000", v)
	}
	return n
}

type idleResult struct {
	connectUsPerConn  float64
	rssPerConn        float64
	rssTotalMiB       float64
	goroutinesPerConn float64
	goroutines        float64
}

type idleStats struct {
	Sessions   int `json:"sessions"`
	Accepted   int `json:"accepted"`
	Goroutines int `json:"goroutines"`
}

func runIdleIteration(b *testing.B, n int) idleResult {
	b.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	addr, pid, stop := startIdleServer(b, ctx)
	// Kill the server before the clients close: the side that closes first keeps the
	// TIME_WAIT entries, and 10k of them on the client side exhaust the ephemeral ports of
	// the next run on macOS.
	closeAll := func() {}
	defer func() { stop(); closeAll() }()

	hc := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	before := fetchIdleStats(b, hc, addr)
	rssBefore := processRSS(b, pid)

	dialer := client.Dialer{Transports: []transport.Transport{websocket.Default}}
	conns := make([]engineio.Conn, n)
	var failed atomic.Int64
	var firstErr atomic.Value
	var readers sync.WaitGroup

	began := time.Now()
	var next atomic.Int64
	var dialers sync.WaitGroup
	for w := 0; w < 32; w++ {
		dialers.Add(1)
		go func() {
			defer dialers.Done()
			for failed.Load() == 0 {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				c, err := dialer.Dial("http://"+addr+"/", nil)
				if err != nil {
					if portsExhausted(err) {
						err = fmt.Errorf("%w (ephemeral ports are exhausted: IDLE_CONNS exceeds the loopback port range, or TIME_WAIT entries of a previous run still hold it, wait a minute)", err)
					}
					firstErr.CompareAndSwap(nil, fmt.Errorf("dial %d/%d: %w", i+1, n, err))
					failed.Add(1)
					return
				}
				conns[i] = c
				readers.Add(1)
				// The client side of an idle session reads, as a real client does; this is
				// also what answers server frames.
				go func() {
					defer readers.Done()
					for {
						_, r, err := c.NextReader()
						if err != nil {
							return
						}
						_, _ = io.Copy(io.Discard, r)
						_ = r.Close()
					}
				}()
			}
		}()
	}
	dialers.Wait()
	connect := time.Since(began)

	closeAll = func() {
		var wg sync.WaitGroup
		for _, c := range conns {
			if c == nil {
				continue
			}
			wg.Add(1)
			go func() { defer wg.Done(); _ = c.Close() }()
		}
		wg.Wait()
		readers.Wait()
	}
	if failed.Load() != 0 {
		b.Fatalf("%v", firstErr.Load())
	}

	// Wait until the server has accepted every session and its goroutine count has
	// stopped moving, so handshake goroutines are not counted as per-session cost.
	var after idleStats
	stable := 0
	prev := -1
	for deadline := time.Now().Add(60 * time.Second); stable < 5; {
		if time.Now().After(deadline) {
			b.Fatalf("server did not settle: %+v, want %d sessions", after, n)
		}
		time.Sleep(200 * time.Millisecond)
		after = fetchIdleStats(b, hc, addr)
		if after.Sessions == n && after.Accepted == n && after.Goroutines == prev {
			stable++
		} else {
			stable = 0
		}
		prev = after.Goroutines
	}
	rssAfter := processRSS(b, pid)

	res := idleResult{
		connectUsPerConn:  float64(connect.Microseconds()) / float64(n),
		rssPerConn:        float64(rssAfter-rssBefore) * 1024 / float64(n),
		rssTotalMiB:       float64(rssAfter) / 1024,
		goroutinesPerConn: float64(after.Goroutines-before.Goroutines) / float64(n),
		goroutines:        float64(after.Goroutines),
	}
	b.Logf("N=%d: server RSS %d -> %d KiB (%.0f B/conn), goroutines %d -> %d (%.2f/conn), connect phase %v",
		n, rssBefore, rssAfter, res.rssPerConn, before.Goroutines, after.Goroutines, res.goroutinesPerConn,
		connect.Round(time.Millisecond))
	return res
}

// startIdleServer re-executes the test binary as the server and returns its address
// and pid; stop kills and reaps it.
func startIdleServer(b *testing.B, ctx context.Context) (addr string, pid int, stop func()) {
	b.Helper()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestIdleBenchServerProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), "IDLE_BENCH_SERVER=1")
	cmd.Stderr = os.Stderr
	// The server exits when its stdin closes, so it does not outlive a crashed parent.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		b.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		b.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		b.Fatal(err)
	}
	var once sync.Once
	stop = func() {
		once.Do(func() {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if a, ok := strings.CutPrefix(sc.Text(), "IDLE_BENCH_ADDR "); ok {
			go func() { _, _ = io.Copy(io.Discard, stdout) }()
			return a, cmd.Process.Pid, stop
		}
	}
	stop()
	b.Fatalf("server subprocess did not report an address: %v", sc.Err())
	return "", 0, nil
}

func fetchIdleStats(b *testing.B, c *http.Client, addr string) idleStats {
	b.Helper()
	resp, err := c.Get("http://" + addr + "/idle-stats")
	if err != nil {
		b.Fatalf("stats: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var s idleStats
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		b.Fatalf("stats: %v", err)
	}
	return s
}

// portsExhausted reports whether a dial error means the ephemeral ports ran out
// (EADDRNOTAVAIL). The transport's dial error does not unwrap to the errno
// (websocket.DialError embeds error without Unwrap), and another WebSocket
// implementation may wrap it differently, so the text of the OS error is matched as
// well: "can't assign requested address" on macOS, "cannot assign requested address"
// on Linux.
func portsExhausted(err error) bool {
	return errors.Is(err, syscall.EADDRNOTAVAIL) ||
		strings.Contains(err.Error(), "assign requested address")
}

// TestPortsExhausted checks the dial-error classification when the errno is lost on the
// way up, as it is in websocket.DialError (embeds error, no Unwrap): only the text is left.
func TestPortsExhausted(t *testing.T) {
	op := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.EADDRNOTAVAIL)}
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"wrapped errno":         {fmt.Errorf("dial: %w", op), true},
		"errno lost, text kept": {fmt.Errorf("dial: %v", op), true},
		"linux text":            {errors.New("connect: cannot assign requested address"), true},
		"other error":           {errors.New("connect: connection refused"), false},
	} {
		if got := portsExhausted(tc.err); got != tc.want {
			t.Errorf("%s: portsExhausted(%v) = %v, want %v", name, tc.err, got, tc.want)
		}
	}
}

// readRSS returns the resident set size of pid in KiB, as reported by ps.
func readRSS(pid int) (int64, error) {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, fmt.Errorf("ps rss of %d: %w", pid, err)
	}
	kib, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("ps rss of %d: %q: %w", pid, out, err)
	}
	return kib, nil
}

// processRSS is readRSS that fails the benchmark.
func processRSS(b *testing.B, pid int) int64 {
	b.Helper()
	kib, err := readRSS(pid)
	if err != nil {
		b.Fatal(err)
	}
	return kib
}

// TestIdleBenchServerProcess is the server half of BenchmarkIdleConnections. It does
// nothing unless IDLE_BENCH_SERVER=1, which only the benchmark sets when it re-executes
// this test binary. It serves an engineio.Server over websocket on a loopback port,
// prints "IDLE_BENCH_ADDR <host:port>" and runs until stdin is closed. Each accepted
// session gets one goroutine that reads and discards frames, the engineio part of the
// read loop that socketio.Server starts per connection.
func TestIdleBenchServerProcess(t *testing.T) {
	if os.Getenv("IDLE_BENCH_SERVER") != "1" {
		t.Skip("helper process of BenchmarkIdleConnections")
	}
	srv := engineio.NewServer(&engineio.Options{
		Transports: []transport.Transport{websocket.Default},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	var accepted atomic.Int64
	go func() {
		for {
			c, err := srv.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			go func() {
				defer func() { _ = c.Close() }()
				for {
					_, r, err := c.NextReader()
					if err != nil {
						return
					}
					_, _ = io.Copy(io.Discard, r)
					_ = r.Close()
				}
			}()
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/", srv)
	mux.HandleFunc("/idle-stats", func(w http.ResponseWriter, _ *http.Request) {
		debug.FreeOSMemory()
		_ = json.NewEncoder(w).Encode(idleStats{
			Sessions:   srv.Count(),
			Accepted:   int(accepted.Load()),
			Goroutines: runtime.NumGoroutine(),
		})
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = http.Serve(ln, mux) }()
	fmt.Printf("IDLE_BENCH_ADDR %s\n", ln.Addr())

	_, _ = io.Copy(io.Discard, os.Stdin)
}
