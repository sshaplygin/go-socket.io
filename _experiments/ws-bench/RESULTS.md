# Exploratory component results — 2026-09-29

The current Gobwas prototype allocates more per echoed message but retains less
Go heap per idle connection than Gorilla in this configuration. Timing varies
substantially between these short runs; there is no demonstrated universal
throughput or latency winner. These are component preparation measurements,
not stage 2.1 acceptance or the stage 6 full-server campaign.

Environment: Apple M1 Max, 10 logical CPUs, macOS 26.2 arm64, Go 1.25.5,
`GOMAXPROCS=4` inherited by the client and each server subprocess. Shell file
descriptor soft limit: 1,048,575. Other project builds/tests were paused during
measurement; this was a normal workstation, not a dedicated load-test host.
No race instrumentation, TLS or compression. Dependencies and implementation
details are pinned/described in [README.md](README.md) and `go.mod`.

Exact commands from this directory:

```sh
go build -o ws-bench .
python3 measure.py --output /tmp/ws-bench-round2-results.json --repeats 3 --idle-connections 1000
cp /tmp/ws-bench-round2-results.json results-darwin-arm64.json
```

[Raw results](results-darwin-arm64.json) contain all 36 successful invocations:
30 echo workloads and six idle workloads. Each invocation uses a fresh server
process. Backend order alternates between repeats. Each connection receives
one acknowledged warmup before measurement. These deliberately short runs are
useful for harness validation and allocation comparisons, not sustained-load
claims or significance testing.

Each table entry is the **median of three run-level metrics**. In particular,
the percentiles below are medians of each run's percentiles, not percentiles
of a pooled sample. One echo means one request plus its complete response.

| Connections | Payload | Backend | Echoes/s | RTT p50 / p95 / p99, µs | Server allocated B/echo | Server mallocs/echo |
|---:|---:|---|---:|---:|---:|---:|
| 1 | 32 B | Gorilla | 33,457 | 26.2 / 55.6 / 74.4 | 521 | 2.01 |
| 1 | 32 B | Gobwas prototype | 32,726 | 25.0 / 60.4 / 81.6 | 4,802 | 5.01 |
| 1 | 1 KiB | Gorilla | 25,511 | 30.1 / 67.3 / 87.9 | 2,826 | 4.01 |
| 1 | 1 KiB | Gobwas prototype | 25,763 | 34.9 / 69.8 / 95.7 | 7,106 | 7.01 |
| 1 | 64 KiB | Gorilla | 5,913 | 151.6 / 274.3 / 338.4 | 285,016 | 19.04 |
| 1 | 64 KiB | Gobwas prototype | 5,866 | 153.7 / 280.3 / 350.5 | 289,240 | 21.03 |
| 16 | 1 KiB | Gorilla | 112,255 | 129.6 / 236.9 / 432.2 | 2,825 | 4.00 |
| 16 | 1 KiB | Gobwas prototype | 107,761 | 131.0 / 259.1 / 477.4 | 7,105 | 7.01 |
| 16 | 64 KiB | Gorilla | 10,950 | 1,108.1 / 3,474.7 / 5,111.7 | 285,015 | 19.05 |
| 16 | 64 KiB | Gobwas prototype | 10,763 | 1,129.7 / 3,519.8 / 5,152.0 | 289,238 | 21.04 |

For example, 16-connection 64 KiB throughput ranged from 10,934 to 11,033
echoes/s for Gorilla and 10,597 to 11,040 for Gobwas; those ranges overlap widely.
The extra roughly 4.2 KiB allocated per Gobwas echo is consistent with the
prototype creating a new writer/buffer per message. This is an interpretation
of the implementation and measurements, not an allocation profile proving
every byte's origin. Stats HTTP/JSON/background allocations are included in
the interval counters and amortized across the measured echoes.

Idle snapshots held **1,000** warmed connections, with forced GC before each
snapshot. Every run verified all target connections remained active:

| Backend | Median Go HeapAlloc delta per connection | Median server goroutine delta |
|---|---:|---:|
| Gorilla | 21,004 B | 1,000 |
| Gobwas prototype | 11,792 B | 1,000 |

This is a point-in-time Go heap observation, not RSS, kernel socket memory,
total machine memory or a stability test. CPU utilization was not measured.
The production Engine.IO 10,000-connection RSS/goroutine comparison remains
outstanding, as does the stage 6 campaign after M6.

Validation performed independently of the performance runs:

```sh
go test -race -count=1 ./...
go vet ./...
golangci-lint run --allow-serial-runners ./...
GOTOOLCHAIN=go1.22.12 go test -ldflags=-linkmode=external -count=1 ./...
```

All passed. The external linker is needed for this host's Go 1.22/macOS dyld
compatibility; the timed executable uses the normal Go 1.25.5 build.
