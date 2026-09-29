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
python3 measure.py --output results-darwin-arm64.json --repeats 3 --idle-connections 1000
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
| 1 | 32 B | Gorilla | 28,014 | 31.7 / 70.4 / 97.9 | 521 | 2.01 |
| 1 | 32 B | Gobwas prototype | 24,621 | 37.0 / 74.8 / 124.4 | 4,802 | 5.01 |
| 1 | 1 KiB | Gorilla | 22,791 | 40.9 / 77.0 / 111.3 | 2,826 | 4.01 |
| 1 | 1 KiB | Gobwas prototype | 18,936 | 45.7 / 95.8 / 165.8 | 7,106 | 7.01 |
| 1 | 64 KiB | Gorilla | 5,465 | 161.9 / 299.8 / 388.5 | 285,016 | 19.04 |
| 1 | 64 KiB | Gobwas prototype | 5,381 | 160.0 / 306.5 / 426.9 | 289,240 | 21.04 |
| 16 | 1 KiB | Gorilla | 97,981 | 143.5 / 291.5 / 519.1 | 2,825 | 4.01 |
| 16 | 1 KiB | Gobwas prototype | 93,431 | 145.5 / 336.5 / 578.1 | 7,106 | 7.01 |
| 16 | 64 KiB | Gorilla | 8,616 | 1,347.8 / 4,595.6 / 6,981.8 | 285,016 | 19.07 |
| 16 | 64 KiB | Gobwas prototype | 9,441 | 1,234.7 / 4,268.2 / 6,312.6 | 289,239 | 21.06 |

For example, 16-connection 64 KiB throughput ranged from 5,885 to 10,101
echoes/s for Gorilla and 6,728 to 9,694 for Gobwas; those ranges overlap widely.
The extra roughly 4.2 KiB allocated per Gobwas echo is consistent with the
prototype creating a new writer/buffer per message. This is an interpretation
of the implementation and measurements, not an allocation profile proving
every byte's origin. Stats HTTP/JSON/background allocations are included in
the interval counters and amortized across the measured echoes.

Idle snapshots held **1,000** warmed connections, with forced GC before each
snapshot. Every run verified all target connections remained active:

| Backend | Median Go HeapAlloc delta per connection | Median server goroutine delta |
|---|---:|---:|
| Gorilla | 21,003 B | 1,000 |
| Gobwas prototype | 11,789 B | 1,000 |

This is a point-in-time Go heap observation, not RSS, kernel socket memory,
total machine memory or a stability test. CPU utilization was not measured.
The production Engine.IO 10,000-connection RSS/goroutine comparison remains
outstanding, as does the stage 6 campaign after M6.

Validation performed independently of the performance runs:

```sh
go test -race -count=1 ./...
go vet ./...
GOTOOLCHAIN=go1.22.12 go test -ldflags=-linkmode=external -count=1 ./...
```

All passed. The external linker is needed for this host's Go 1.22/macOS dyld
compatibility; the timed executable uses the normal Go 1.25.5 build.
