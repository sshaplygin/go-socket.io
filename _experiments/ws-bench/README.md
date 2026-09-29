# WebSocket echo and idle comparison

This standalone Go 1.22 module compares Gorilla WebSocket **v1.5.3** against
the [existing Gobwas framing prototype](../eio4-websocket) using Gobwas **v1.4.0**.
It does not change the root module or compare Engine.IO/Socket.IO versions.
The relative `replace` requires the sibling prototype directory when copying or
merging this experiment. No runtime switch to Gobwas is implied by these results.

This is component regression/preparation work for roadmap stage **2.1**. It does
not close that stage's performance acceptance: the integrated production
Engine.IO implementation still needs the before/after 10,000-connection **RSS**
and goroutine benchmark. This harness measures a framing server's Go heap,
which is not RSS, and contains no production Engine.IO integration.

It also does not implement or satisfy roadmap **stage 6**, the final full-server
campaign after M6. That campaign requires actual Socket.IO clients on separate
load-generator resources, open-loop load, its broader workload matrix, and at
least 30 seconds of warmup, 120 seconds of measurement and ten independent runs
per published result. The small closed-loop component measurements here have a
different purpose and cannot substitute for that acceptance evidence. This
scope was checked against the updated roadmap snapshot with SHA-256
`29fb1a18030e7277ee1b43839d70b58d72e373bbb835ab1f93eef5b85ad56a28`.

See [RESULTS.md](RESULTS.md) for the initial workstation measurements and raw
JSON. Their short duration and observed variability limit timing conclusions.

Build and validate from this directory:

```sh
go mod verify
go test -race -count=1 ./...
go vet ./...
go build -o ws-bench .
```

The root `go test ./...` does not discover this separate module. Never use a
race-instrumented executable for performance results. Smoke tests cover both
implementations, empty/maximum messages, counts/percentiles, incorrect echoes,
validation, stats errors, startup failure, subprocess cleanup and cancellation.

On newer macOS releases, Go 1.22's internal linker can produce a binary rejected
by dyld for a missing `LC_UUID`. The compatibility check can use
`GOTOOLCHAIN=go1.22.12 go test -ldflags=-linkmode=external -count=1 ./...` with the
platform C toolchain installed; this does not alter benchmark source behavior.

## Run one workload at a time

Each invocation starts a fresh server subprocess and kills and reaps it on
completion or failure. Clients and orchestration run in the parent process.
Both processes run on the same machine, so CPU scheduling contention still
affects timing even though server heap metrics exclude client allocations.

```sh
GOMAXPROCS=4 ./ws-bench -backend gorilla -connections 1 -messages 10000 -size 1024 > gorilla.json
GOMAXPROCS=4 ./ws-bench -backend gobwas-prototype -connections 1 -messages 10000 -size 1024 > gobwas.json
GOMAXPROCS=4 ./ws-bench -backend gorilla -connections 16 -messages 2000 -size 65536 > gorilla-concurrent.json
GOMAXPROCS=4 ./ws-bench -backend gobwas-prototype -connections 16 -messages 2000 -size 65536 > gobwas-concurrent.json
GOMAXPROCS=4 ./ws-bench -backend gorilla -mode idle -connections 10000 -timeout 120s > gorilla-idle.json
GOMAXPROCS=4 ./ws-bench -backend gobwas-prototype -mode idle -connections 10000 -timeout 120s > gobwas-idle.json
```

Idle target 10,000 is configurable, not a claim that every environment can open
it. Each process needs roughly that many file descriptors; loopback ephemeral
ports and OS socket memory also constrain the run. Failure to reach the target
is an error, not a partial-success result. Reduce `-connections` or provision
limits explicitly. The whole run has a deadline (default 60 seconds), including
startup, sequential dialing, warmup and measurement. No workloads run forever.
The CLI limits payloads to 1 MiB, connections to 100,000, and echo samples to
10 million. Repeat runs and alternate backend order; do not run builds or other
benchmarks simultaneously. Record OS, hardware, Go version and environment.

For the checked-in workload matrix, Python 3's standard library is sufficient:

```sh
python3 measure.py --output measurements.json --repeats 3 --idle-connections 1000
```

This performs five echo configurations and an idle configuration per backend
per repeat, sequentially, alternating backend order between repeats and setting
`GOMAXPROCS=4` in both processes. Output creation is exclusive to preserve prior
results. Select a larger idle target explicitly when host resources permit.

## What is held equal

- TCP loopback, HTTP upgrade, binary messages, no TLS/compression/subprotocols.
- One identical Gorilla client implementation and 4 KiB client read/write
  buffers for both servers; one request in flight per connection.
- Complete owned message reads followed by echo writes; 1 MiB read limits.
- Nominal 4 KiB buffering: Gorilla explicit read/write sizes, Gobwas HTTP
  upgrade reader and the prototype's default `wsutil` writer. These APIs do
  **not** have identical buffer lifetimes/layouts. Gobwas's writer buffer also
  reserves frame-header space. This is an implementation comparison, not a
  claim of identical internal algorithms.
- The prototype uses `io.ReadAll` and creates a `wsutil.Writer` per message;
  Gorilla retains write buffers on the connection. No pools, prepared messages,
  custom framing fast paths or library modifications are added by the harness.
- Identical payload sizes/content and exact opcode/content validation on every
  echoed message. Binary avoids differing UTF-8 validation paths.

## Reading JSON output

**Echo mode:** one untimed acknowledged warmup per connection, then a server
GC/baseline snapshot, then concurrent closed-loop clients. Timing starts when
their shared start gate opens and ends after all finish. `completed_echoes` is
the total across connections; each echo contains one request and one response.
Throughput is completed echoes per wall-clock second, not frames per second.
Payload MiB/s counts both directions and excludes protocol headers. RTT samples
measure client write through complete response read, excluding subsequent
content validation. p50/p95/p99 use nearest-rank percentiles of all individual
RTTs, not percentiles of per-connection averages. Validation is included in
workload elapsed time. Dialing, warmup, snapshots and sorting are excluded.

This closed-loop echo load reports observed RTT under its chosen concurrency;
it does not measure open-loop saturation, correct coordinated omission, or
predict broadcast/server-push latency. The client can become a bottleneck.

`server_interval_*_per_echo` divides the subprocess `runtime.MemStats`
TotalAlloc/Mallocs deltas by successful echoes. It includes small stats HTTP/JSON
and background runtime costs between snapshots; it is **not** a microbenchmark
of just `ReadMessage`/`WriteMessage`. Use sufficiently many messages to amortize
that fixed cost. The ending echo snapshot does not force GC. GC count is
reported, but pause times, CPU utilization and RSS are not measured.

**Idle mode:** snapshots are taken after forced GC before opening connections
and while every successfully warmed connection remains open. Heap delta per
connection uses `HeapAlloc`, not `HeapInuse`, RSS, kernel socket memory or total
system memory. Gorilla's retained write buffers and Gobwas's discarded writer
buffers are deliberately visible. The goroutine delta includes server handlers;
the same persistent stats HTTP connection is warmed before the baseline.
No timed idle hold or long-lived stability claim is made. Zero echo/latency
fields in idle output are inapplicable; idle delta fields in echo output are
likewise inapplicable. Negative heap deltas are retained rather than clamped.

The benchmark is evidence for this prototype and configuration only. A lower
number here does not establish a general library performance ranking or replace
full Engine.IO session, heartbeat, polling-upgrade and broadcast measurements.
