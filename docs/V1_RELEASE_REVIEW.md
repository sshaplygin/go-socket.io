# V1 release review

This review checks whether the v1 module `github.com/sshaplygin/go-socket.io` is ready
to be published as `v1.5.0`. It was carried out on 2026-10-21 against `master` at
`7311fbe`. The `v2/` module is out of scope.

## Method

Eight independent reviews were run, one per dimension:

1. clarity
2. architecture
3. tests
4. Go API compatibility
5. protocol interoperability
6. performance
7. correctness, concurrency and security
8. release engineering

Every blocker and major finding was then re-checked by a separate reviewer. The re-checker
tried to refute the finding: they re-read the code, re-ran the commands and, where
possible, reproduced the defect. Minor and nit findings were not re-checked. A final pass
merged duplicates across dimensions and resolved contradictions.

Severity levels:

- **blocker:** must be fixed before `v1.5.0` is tagged.
- **major:** should be fixed before the tag, or deferred explicitly in writing.
- **minor:** fix soon after the release.
- **nit:** cosmetic.

Finding IDs (`CL-`, `ARCH-`, `T`, `API-`, `interop-`, `PERF-`, `C`, `REL-`) refer to the
[finding catalogue](#finding-catalogue).

## Verdict: ready after fixes

The engineering base is strong:

- Every repository gate passes: lint, vet, architecture, parity, coverage at 93.6%, race,
  stress and examples.
- The conformance suite passes 62/62 with both `socket.io-client` 1.7.4 and 2.5.0.
- Three decoders ran for millions of fuzz executions without a crash.
- The package graph matches [ARCHITECTURE.md](ARCHITECTURE.md).

One confirmed blocker remains: [B1](#b1-permanent-session-deadlock).

Seven major findings should be fixed before the release or explicitly deferred:

- five concern documentation and release communication (M1–M3, M6, M7);
- two are runtime issues (M4, M5).

`v1.5.0` is the first usable release of this module path, so it also fixes the public
API surface for the life of v1. The decisions in
[Decisions required before tagging](#decisions-required-before-tagging) have to be made
before the tag.

## Scorecard

| Dimension | Score | Verdict | Reason |
| --- | --- | --- | --- |
| Clarity | 5/10 | ready with fixes | **Good:** reference docs are accurate and every `V1.md` snippet compiles. **Gaps:** there is no quick start or migration note, 9 of 12 packages have no package doc, and the `OnError` and Redis callback contracts are documented incorrectly. |
| Architecture | 7/10 | ready with fixes | **Good:** the package graph is clean, there is no dead code, and a consumer of the root package depends only on gorilla/websocket. **Gaps:** the adapter, alias and typed-event contracts need decisions before the API freeze. |
| Tests | 7/10 | ready | **Good:** coverage is 93.6%, tests check behavior, and race, stress and fuzz runs are green. **Gaps:** there is no goroutine-leak gate, and no scenario has a non-reading client that keeps pinging, which is the B1 trigger. |
| API compatibility | 4/10 | not ready until migration docs exist | **Status:** semver holds formally, because the module path is new. **Problem:** the upstream API breaks, the changed defaults and the Redis wire break have no migration notes. |
| Interoperability | 8/10 | ready with fixes | **Good:** 62/62 conformance and 7/7 `examples-node` with each pinned client. **Gaps:** `forceBase64` over websocket is not supported, and several wire differences are undocumented. |
| Performance | 6/10 | ready with fixes | **Good:** outbound queues are bounded and slow consumers are evicted cleanly. **Gaps:** the JSON encoder from #143 regresses broadcast cost, Redis `PUBLISH` can block indefinitely, and nothing benchmarks Socket.IO-level code. |
| Correctness and security | 6/10 | not ready | **Good:** decoders are robust, limits are on by default, and race runs are clean. **Problem:** any client can trigger a session deadlock (B1). |
| Release engineering | 7/10 | ready with fixes | **Good:** CI is green on `7311fbe` and `go.mod` is tidy. **Gaps:** release notes are missing, the version number and inherited tags need a decision, and the stress job never runs on push. |

## Release blockers

### B1. Permanent session deadlock

`C1`, confirmed as a blocker by three independent reproductions.

**Lock cycle.** Three paths in the Engine.IO session wait on each other's locks.

| Lock | Holder | Waiter |
| --- | --- | --- |
| `ws.writeLocker` | `serveWrite`, during an in-flight websocket write | `setDeadline` (`engineio/internal/session/session.go:489-501`), which calls `conn.SetWriteDeadline`; that setter takes the lock in `engineio/transport/websocket/connect.go:101` |
| `upgradeLocker`, read side | `setDeadline`, while it waits for `ws.writeLocker` | `closeMode` (`engineio/internal/session/close.go:46`), which requests the write side |
| `upgradeLocker`, read side (new reader) | — | `Session.fail` (`close.go:141`), called by `serveWrite` while it still holds `ws.writeLocker` |

`fail` cannot take the read lock because `closeMode` already has a write lock request
pending, and a pending writer on a Go `RWMutex` blocks new readers. None of the three paths
can make progress.

**Trigger.** A websocket client, including one upgraded from polling, stops reading but
keeps sending pings while the server emits. The 64-packet outbound queue overflows, and the
resulting `Abort` enters the cycle. A client that also stops pinging is disconnected
normally; that control run closed in 8 µs.

**Evidence.** Each reproduction logs `write buffer full`, then no `OnDisconnect` within
30 s, then `close timed out err="1 connections still open"`. `Server.Close` returns after
its 10 s bound. Every run shows the same four blocked stacks. At the Engine.IO level,
`Server.Close` was still blocked when the test timed out at 2m30s.

**Impact.**

- `OnDisconnect` never runs, and the socket never leaves its rooms.
- The connection's goroutines and its TCP connection leak permanently.
- `engineio.Server.Close` (`engineio/server.go:88-90`) shuts sessions down one at a time,
  so it never reaches the sessions after the stuck one, nor `dropUnaccepted`.

**Fix.**

1. In `setDeadline`, read `s.conn` under the read lock, release the lock, and only then
   call the deadline setters. `nextReader` and `nextWriterCompress` already work this way.
2. Make `Session.fail` independent of `upgradeLocker`, for example by keeping `closed` and
   `conn` in atomic snapshots.
3. Optional: store the websocket write deadline atomically and apply it inside
   `nextWriter`, so `SetWriteDeadline` never waits for an in-flight write.
4. Shut sessions down concurrently in `engineio.Server.Close`, or bound each shutdown in
   time.
5. Add a `-race` regression test in which a pinging client that does not read receives
   large emits, for both direct websocket and the polling-to-websocket upgrade. It must
   assert that `OnDisconnect` fires and that `Abort` and `Server.Close` return promptly.

## Should fix before release

### M1. No migration guide or release notes

`API-1`, downgraded from blocker to major; `CL-4` major; `REL-1`.

**Where the guide went.** `b2c1cd2` deleted `CHANGELOG.md` (602 lines) and
`docs/V1_MIGRATION.md` (453 lines). `CONTRIBUTING.md:24-27,56` now states that v1 needs no
changelog.

**Why it matters.** Every adopter of `v1.5.0` arrives from upstream, because the fork's own
`v0.9.1`–`v1.4.2` tags declare `module github.com/googollee/go-socket.io` and cannot be
used under this path. `apidiff` reports these incompatible changes:

| Baseline | Package | Incompatible changes |
| --- | --- | --- |
| `v1.4.2` | root | 24 |
| upstream `v1.7.0` | root | 20 |
| upstream `v1.7.0` | `engineio` | 17, plus 5 removed packages |

**Changes that compile but change meaning:**

- A non-root `Conn.ID()` now returns `<nsp>#<sid>` (`socket.go:257-263`). The public godoc
  in `types.go:71` does not say so.
- `BroadcastToRoom(room, event, arg)`, written in the `v1.4.2` style, still compiles but
  sends nothing (`API-8`).

**Fix.** Add a "Migrating from googollee/go-socket.io" section to [V1.md](V1.md), and
publish the same text as the GitHub release notes. It should cover:

- the import path;
- an old-to-new identifier table, for example `NewServer(*engineio.Options)` →
  `NewServer(...Option)` and `Server.Adapter(*RedisAdapterOptions)` →
  `WithAdapter(redis.New(...))`;
- a changed-defaults table, see the table below;
- the Redis upgrade procedure from [M3](#m3-redis-adapter-is-wire-incompatible-with-upstream-go-peers);
- the Go 1.22 floor (`API-13`).

Also state the ID format in the `Conn.ID` godoc of both roles.

| Changed default | Covered by |
| --- | --- |
| CORS | [M2](#m2-permissive-cors-default-is-not-in-the-user-docs) |
| Cookie | migration notes |
| Compression | migration notes |
| Ping interval: 80 s → 45 s | migration notes |
| Message size limit: 1e6 bytes | migration notes |
| Disconnect reasons | migration notes |
| `Conn.ID` format | migration notes and `Conn.ID` godoc |
| JSONP: removed | migration notes |

This guide documents the current contract plus a one-time migration, so it does not
conflict with the no-changelog policy.

### M2. Permissive CORS default is not in the user docs

`API-3` confirmed major; duplicates `CL-9`, `C5` and `interop-9`.

**Behavior.** `engineio/options.go:170-176` accepts every origin by default:

- Polling reflects the request `Origin` and sets `Access-Control-Allow-Credentials: true`.
- Websocket skips the origin check entirely.

**Probe.** A default server answered a request with `Origin: https://evil.example` as
follows:

| Server | Polling | Websocket |
| --- | --- | --- |
| Default | `Access-Control-Allow-Origin: https://evil.example`, credentials allowed | `101` |
| `WithCORS(false)` | — | `403` |

Upstream `v1.7.0` (go-engine.io `v1.4.1`) answered `403` by default.

**Documentation.** The default matches socket.io 2.x and was a deliberate decision. It is
documented only in godoc: README, V1.md and PROTOCOL.md never mention it. Applications that
authenticate with cookies are exposed to cross-site WebSocket hijacking without knowing it.

**Fix.**

- Add a CORS section to [PROTOCOL.md](PROTOCOL.md) covering the default, preflight handling,
  `WithCORS(false)` and `CheckOrigin`.
- Add a security note to V1.md and to the migration notes.
- Remove the allow-all `CheckOrigin` from `_examples/default-http/main.go:19-43`, or replace
  it with an origin allow-list.

### M3. Redis adapter is wire-incompatible with upstream Go peers

`API-4`, confirmed major.

**What changed.** The default prefix is still `socket.io` on both sides, but the channel
names and the payload codec are different:

| Item | Upstream | Fork |
| --- | --- | --- |
| Subscription pattern | `%s#%s#*` | `%s#*` |
| Key | `%s#%s#%s` | `%s#%s#` |
| Request channel | `%s-request#%s` | `%s-request#%s#` |
| Codec | JSON | MessagePack (`codec.go:28-33`) |

The patterns are in `adapter/redis/adapter.go:121-124`.

**Effect in a mixed fleet.** Verified on a real Redis server, the old and new patterns
match each other's channels:

- A new instance discards decode errors (`subscription.go:208`), so it simply misses old
  traffic.
- An old instance's `dispatch()` returns on the first decode error. After it receives one
  fork message, it stops processing all cluster traffic.

**Fix (documentation only).** Give the new fleet a distinct `WithPrefix`, or upgrade every
instance at once. Put this procedure in PROTOCOL.md and in the migration notes. The deleted
changelog carried this warning.

### M4. Redis `Send` and `SendAll` drop broadcasts silently

`ARCH-1` confirmed major; `CL-2` (downgraded to minor) resolved as major.

**Behavior.** `adapter/redis/adapter.go:154-161` discards the `SendTo` error. As a result:

- With a trailing callback, nothing is delivered, not even to local sockets.
- When Redis is down, cross-node delivery is lost and nothing is logged.
- `BroadcastToRoom` and `BroadcastToNamespace` still return `true`.

**Contract violations.**

- The logger contract says WARN "marks a failure no caller receives"
  (`logger/logger.go:31-32`). `Clear` already follows it, but `Send` and `SendAll` do not.
- Godoc promises per-socket callbacks in `server.go:275-290`, `errors.go:21-22`,
  `adapter/adapter.go:61-62` and the Redis `Send` comments.

**Why major despite the downgrade.** The `CL-2` re-check downgraded the finding because
V1.md:125-126 mentions that callbacks are rejected. It did not consider the silent loss
when Redis is down.

**Fix.**

- Log WARN in `Send` and `SendAll` when `SendTo` fails.
- Correct the godoc: the boolean result only reports that the namespace exists.

### M5. Redis `PUBLISH` has no I/O deadline

`PERF-3`, confirmed major.

**Behavior.** The pool's dial options (`adapter/redis/adapter.go:70-104`) set no read or
write timeout. `codec.go:191-198` calls `c.Do` synchronously on the caller's goroutine.

**Reproduction.** With `redis-server` stopped by `SIGSTOP`:

- `SendTo` was still blocked after 15 s.
- Closing the adapter did not release it.
- It returned only after `SIGCONT`, at 20.0 s.

Until TCP keepalive gives up, about 5 minutes, every broadcasting handler hangs, and with
it the handler's connection.

**Fix.** Set `DialReadTimeout` and `DialWriteTimeout`, or use `DoContext`. Bound both by the
requests timeout.

### M6. JSON encoder from #143 regresses encode cost

`PERF-1`, confirmed major.

**Cause.**

- `parser/json_encode.go:109-182` calls `json.Marshal` once for every leaf value and every
  map key, and sorts the entries.
- `parser/encoder.go:138` allocates a map for every packet.

**Measurements.**

| Benchmark | Baseline | Result |
| --- | --- | --- |
| Encode a slice of 100 structs: allocations | 1,004 (`v1.4.2`), 1,012 (previous master) | 2,518 |
| Encode, independent re-check | 13 allocations, about 85 µs | 1,419 allocations, about 150 µs |
| Broadcast 100 structs to 10 websocket clients, end to end | 1.535 ms (previous master) | 2.279 ms: +48.5%, p = 0.000; allocations +145% |
| Encode a small single struct | previous master | about 8% slower |

**Fix.**

- Add a cached fast path per `reflect.Type`: when a subtree contains no `Buffer`, no
  interface and no custom marshaler, encode it with a single `json.Marshal`.
- Write scalar values with append-style writers.
- Create the cycle-detection map lazily.
- Add parser encode benchmarks (`PERF-10`).

If the fix is deferred, state the cost in the release notes.

### M7. No quick start

`CL-1`, confirmed major.

**Gap.** The `v1.4.2` README had a quick start at lines 33-82. The current README shows
only `go get`. V1.md never mounts the handler or calls `Serve`, `Close` or `Connect`, and
the repository has no `Example` functions.

**Failure mode.** A user who forgets `go server.Serve()` gets no error: `Connect` returns
nil, but `OnConnect` never fires and acknowledgements never arrive.

**Fix.**

- Add a 20–30 line server and client quick start to the README or the root package doc.
- Add runnable `Example` functions.
- State in the `Server` and `Serve` godoc that `Serve` must be running before connections
  are dispatched.

## Decisions required before tagging

`v1.5.0` freezes these choices for the life of v1.

**Version number** (`API-2`, `REL-7`)

- `v1.5.0` reuses an upstream release number and is lower than upstream `v1.7.0`, whose
  history this module contains.
- Decide: keep `v1.5.0` and say in the release notes that the fork does not continue
  upstream numbering, or choose a number above upstream, such as `v1.9.0`.

**Inherited tags** (`CL-10`, `REL-6`)

- Today `go get github.com/sshaplygin/go-socket.io@latest` fails with a module path mismatch.
- Add `retract [v0.9.1, v1.4.2]` to `go.mod` in the release commit. Those tags declare the
  upstream module path.

**Aliases to internal packages** (`ARCH-2`, `API-9`)

- 23 public aliases, among them `engineio.Conn`, `CloseError`, `RemoteError` and
  `EventHandler`, hide their methods and fields from `go doc`.
- This contradicts the rule in `internal/parity/api_docs_test.go:15-16`.

**Typed events** (`ARCH-3`, `CL-11`)

- Typed client events live in the server package, so a typed Go client must import the
  server.
- `client.RegisterEvent`, `RequestAck` and `SendEvent` expose types from `internal/event`.

**Adapter contract** (`ARCH-5`, `CL-7`)

- `Send` and `SendAll` duplicate `SendTo`, and `Rooms(nil)` overloads `AllRooms`.
- The optional hooks `io.Closer` and `BindSocketLookup` are detected by duck typing.
- The `AllRooms` comment contradicts the implementation.

**Engine connection access** (`ARCH-6`)

- `Conn.EngineConn()` exposes `NextReader` and `NextWriter`, which the library itself owns.

**Exported test helpers** (`ARCH-11`, `API-12`)

- `NewFakeConn*`, `FakeDiscardWriter` and `TimestampFromClock` are exported.
- One doc comment refers to a nonexistent `base.Conn`.

## Backlog

Each item has an ID in the [finding catalogue](#finding-catalogue).

### Correctness and robustness

- **Dynamic namespaces are never removed** (`C2` downgraded to minor, `ARCH-9`).
  - Namespaces created through `OfFunc` stay in the registry: 20,000 of them took 25.7 MB.
    They stay even when every connection to them is rejected.
  - socket.io 2.x behaves the same way. Document the retention in the `OfFunc` godoc and
    V1.md, and consider a cleanup option later.
- **Duplicate session IDs** (`C3`). A duplicate SID from a custom `IDGenerator` replaces a
  live session, and `Server.Close` then leaves the first session open.
- **`OnError` contract** (`CL-3` downgraded to minor, `C4`).
  - `OnError` receives a nil `Conn` for root rejections. Neither `OnError` method documents
    this, and V1.md:41 states the opposite.
  - A panic inside `OnError` is recovered without any log (`namespace_handlers.go:223`).
- **Frame reader not closed on errors** (`T2`). After a parse error or a mid-frame failure,
  the websocket frame reader stays open, together with its goroutine and a 30 s timer.
- **Polling timers** (`PERF-2` downgraded to minor). The polling transport creates a
  `time.After` timer for every packet. With pre-1.23 timer semantics, each one keeps about
  1 KiB alive for about 45 s. go-engine.io `v1.4.1` had the same code, so this is not a
  regression.

### Interoperability

- **Base64 over websocket** (`interop-1` downgraded to minor). The websocket transport
  ignores `b64`, so a `forceBase64` client loses its session when it sends binary data.
  Document this in PROTOCOL.md before the release.
- **Undocumented wire differences:**
  - every JSON payload and the OPEN packet end with `\n` (`interop-2`);
  - polling `POST` accepts fewer `Content-Type` values than the reference server
    (`interop-3`);
  - ERROR payloads are always strings (`interop-4`);
  - an untyped handler whose argument type does not match sends no ACK, so the JS callback
    never fires (`interop-5`);
  - an unknown packet type closes the connection (`interop-9`);
  - no close frame is sent when the server closes a websocket (`interop-6`).

### Documentation

- 9 of 12 packages have no package doc, and lint disables `ST1000` and `ST1020`–`ST1022`
  (`CL-5`).
- Several server APIs overlap but differ in ACK semantics (`CL-8`).
- Close, drain and upgrade have no written state-machine description (`CL-12`).
- ARCHITECTURE.md undercounts the per-connection workers. The measured count is 7
  goroutines per connection (`ARCH-13`, `PERF-13`).
- Registering a handler can block for 10 s when Redis is unreachable (`T4`).
- PARITY.md says conformance covers the heartbeat, but no conformance test sends a ping
  (`T3`, `interop-7`).
- Redis log records use the key `namespace` instead of `nsp` (`ARCH-12`).
- `redis.Cluster` methods have unnamed parameters (`CL-6`).
- Contributor build instructions live only in CLAUDE.md (`CL-14`).

### Performance

- The Redis pool has `MaxIdle` 4 and no `MaxActive` limit: it opened 2,259 TCP connections
  for 12,800 publishes (`PERF-4`).
- The Go polling client shares `http.DefaultTransport`. Ten clients in one process exhaust
  ephemeral ports (`PERF-5`).
- A broadcast encodes the packet once per recipient (`PERF-6`).
- `WithHeader` copies the packet even when no ACK ID was added (`PERF-7`).
- The websocket wrapper starts a goroutine and a 30 s timer for every frame (`PERF-8`).
- `Server.JoinRoom` scans every socket in the namespace (`PERF-9`).
- Decoding is 42% slower than in `v1.4.2`. It is unchanged from the previous master, so it
  is not a regression from #143 (`PERF-11`).
- A single-room broadcast still builds a dedup map (`PERF-12`).

### Tests and CI

- `omitempty` handling (`isJSONEmpty`) has no test coverage (`T1`).
- No test fails when goroutines leak (`T2`).
- No fuzz target covers encoding or an encode/decode round trip (`T5`).
- Some tests still synchronize with sleeps (`T6`).
- No benchmark covers the Socket.IO level (`PERF-10`).
- No CI job builds with `GOEXPERIMENT=jsonv2` (`ARCH-10`).
- No `apidiff` or `gorelease` gate protects v1 after the release (`API-11`).
- The stress job never runs on push or on schedule, although `ci.yaml:17` and CLAUDE.md say
  it does (`REL-4`).
- Branch protection requires only three checks (`REL-5`).
- The test packages do not compile for 386 (`REL-8`).
- golangci-lint is unpinned (`latest`), and some actions target the deprecated Node 20
  runtime (`REL-9`).

### Release hygiene

- The README advertises `@v1.5.0` before the tag exists, and the `engineio` pkg.go.dev link
  is missing (`CL-10`, `REL-6`, `API-10`).
- A previously deleted `v1.5.0` is still in the maintainer's local module cache. Purge it
  before running the consumption check (`REL-2` downgraded to minor).
- The repository-local git identity is a placeholder (`Probe <probe@example.invalid>`). It
  has already appeared in a `Co-authored-by` trailer on master, and it would become the
  tagger (`REL-3`).
- Example modules pin an old `golang.org/x/net` and contain module path typos such as
  `deault-http` (`REL-10`, `CL-13`).
- 130 remote branches remain, including the superseded `release/v1.5.0` (`REL-11`).
- An open TODO remains at `engineio/transport/websocket/connect.go:99` (`REL-12`).

### Maintainability

- **Unused write path** (`ARCH-4`). The `boundary` / `writeChan` path is never used but costs
  about 5.5 KiB per connection.
- **Duplicated runtimes** (`ARCH-7`). The root and client runtimes contain 15 clone groups.
- **Complexity hotspots** (`ARCH-8`):
  - `probeTransport` has cyclomatic complexity 45, is 219 lines long, and resumes with a
    1 ms busy-wait;
  - the connection close condition is a compound expression rather than an explicit state.

## Downgraded findings

No finding was fully refuted. The re-checks and the final pass changed these severities:

| Finding | Change | Reason |
| --- | --- | --- |
| `API-1` | blocker → major | The no-changelog policy is deliberate, the module path is new, and the compile errors are loud. Merged into M1. |
| `REL-1` | major → minor | Re-checked as minor. The merged finding M1 stays major because of the silent default and wire changes. |
| `CL-2` | major → minor | V1.md:125 documents that callbacks are rejected. Raised back to major in M4 because of the silent loss when Redis is down. |
| `CL-3` | major → minor | The `Conn.Close` and `ErrWriteBufferFull` godoc mention the nil `Conn`, `recover` prevents a crash, and no example dereferences it. |
| `API-2` | major → minor | The number is valid for a new module path, and `@latest` resolves once the tag exists. |
| `API-5` | major → minor | Most defaults are documented in PROTOCOL.md, and the constructor change prevents silent compilation. The `Conn.ID` gap is part of M1. |
| `REL-2` | major → minor | The module proxy and sumdb never saw the deleted tag, so the risk is limited to one local cache. |
| `PERF-2` | major → minor | go-engine.io `v1.4.1` has identical code. The effect is transient and applies only to pre-1.23 timer semantics. |
| `C2` | major → minor | Matches socket.io 2.x. A match function and root authentication can mitigate it, and `OfFunc` is a new opt-in API. |
| `interop-1` | major → minor | Requires an opt-in `forceBase64` client sending binary data, and is not a regression. |

The re-checks confirmed `CL-1`, `CL-4`, `ARCH-1`, `API-3`, `API-4`, `PERF-1` and `PERF-3` as
major, and `C1` as a blocker.

## Validation results

All runs were on darwin/arm64 with Go 1.25.5 unless noted.

### Tests and coverage

| Command | Result |
| --- | --- |
| `make test` | 16 packages ok, 19.5 s |
| `make test-race` | all packages ok, 35.4 s |
| `make test-stress` (race, `-count=5`, `-shuffle`, `-cpu=1,4`) | ok in two separate runs, 3m09s–3m17s |
| `go test -race -count=4 -shuffle=on` over root, `client/...`, `engineio/...`, `adapter/...` | ok |
| `go test -json` | 601 top-level tests, 0 failed; slowest `TestRedisServerCloseStopsBroadcasts` at 11.32 s |
| `make cover-check` | 93.6% (floor 91.7%), 0 uncovered exported functions |

Statement coverage of each package's own code:

| Package | Coverage |
| --- | --- |
| root | 95.1% |
| `client` | 92.5% |
| `engineio` | 92.9% |
| `adapter/redis` | 91.9% |
| `parser` | 88.0% |

### Fuzzing

No crashers were found.

| Target | Executions |
| --- | --- |
| `FuzzDecoderDiscardPacket` | 883,983 |
| `FuzzMalformedPollingPayload` | 4,208,188 |
| `FuzzRedisBroadcastDecoder` | 1,803,566 |
| Additional Socket.IO decoder probe | about 5.6 million |
| Additional payload decoder probe | 2.41 million |

### Interoperability runs

| Check | Result |
| --- | --- |
| `make conformance`, `socket.io-client` 1.7.4 | 62 pass / 0 fail, 48.5 s |
| `make conformance`, `socket.io-client` 2.5.0 | 62 pass / 0 fail, 47.5 s; 49.2 s on rerun |
| `make examples-node` | 7/7 per client |
| Heartbeat soak, 200 ms interval | 14 pongs, 0 disconnects, 6/6 runs |
| `forceBase64`, reference server | 12/12 |
| `forceBase64`, Go server, polling | 4/4 |
| `forceBase64`, Go server, websocket and upgrade | 0/8 |

### Lint, gates and vulnerabilities

| Check | Result |
| --- | --- |
| `make lint` | 0 issues |
| `make vet`, `make gates`, `make sync-check`, `make examples` | pass; 15 example modules |
| `govulncheck`, local Go 1.25.5 | 26 called standard library vulnerabilities (toolchain only), 0 third-party |
| `govulncheck`, CI Go 1.27.3 | none |
| Go 1.22.12 build and vet | ok |
| Go 1.21.13 build | fails on `reflect.TypeFor`, consistent with the 1.22 floor |
| `GOARCH=386` vet | fails with a test constant overflow (`REL-8`) |

### API

| Check | Result |
| --- | --- |
| `apidiff`, `v1.4.2` → `7311fbe`, root | 24 incompatible, 37 compatible |
| `apidiff`, upstream `v1.7.0` → `7311fbe`, root | 20 incompatible, 33 compatible |
| `apidiff`, upstream `v1.7.0` → `7311fbe`, `engineio` | 8 incompatible, 36 compatible |
| `go get ...@latest` | fails: module path mismatch |
| `go get ...@v1.5.0` | fails: unknown revision |

### Performance measurements

| Measurement | Result |
| --- | --- |
| Encode a slice of 100 structs, allocations (`v1.4.2` / previous master / `7311fbe`) | 1,004 / 1,012 / 2,518 |
| Broadcast 100 structs to 10 clients, end to end | +48.5% |
| Decode | +42% vs `v1.4.2`, unchanged vs previous master |
| Polling heap over 200,000 packets, Go 1.22 timer semantics | 3.8 → 217.9 MiB; 4.4 MiB with `asynctimerchan=0` |
| Goroutines per idle websocket connection | 7.00 (`v1.4.2`: 4) |
| One non-reading client and 10 reading clients over 20,000 broadcasts of 4 KiB | fast clients received 200,000/200,000, the slow client was evicted, heap grew by 0.3 MiB |

## Strengths

- **Architecture is enforced, not just described.**
  - ARCHITECTURE.md matches `go list` edge for edge, and `.github/architecture` checks it.
  - Codecs import only `logger`, and `client` never imports the server.
  - `staticcheck -checks U1000` and `deadcode -test` report nothing.
- **Tests check behavior.**
  - 672 test functions assert wire frames, disconnect reasons and errors.
  - Tests that need external services are gated: miniredis is used by default, and the
    conformance tag fails rather than skips when dependencies are missing.
- **Interop is tested against the reference implementation.**
  - The differential suite compares Go and Node socket.io 2.5.0 transcripts across 12
    scenarios on both transports.
  - It runs with both pinned clients, a mixed Go/Node Redis cluster and permessage-deflate.
- **Hostile input is handled.**
  - Length and ID parsing is overflow-safe.
  - Attachments are capped at 10, and inbound messages at 1e6 bytes, including inflated
    websocket data.
  - Session IDs come from `crypto/rand`.
  - MessagePack input is validated before allocation.
- **Outbound queues are bounded.** In the 20,000-broadcast run above, the slow client was
  evicted without affecting the others. Teardown releases goroutines and room memberships,
  which `v1.4.2` leaked.
- **Docs are accurate where they exist.**
  - Every V1.md snippet compiles, and the eight documents contain 0 broken links.
  - PROTOCOL.md lists deviations explicitly, and godoc defaults match the code.
- **Release mechanics are in place.**
  - `go.mod` is tidy and verified for the root module and 15 examples.
  - The library cross-builds for linux, windows, darwin, freebsd, wasm and 386.
  - CI run 39589689698 on `master` is green, including `govulncheck` on Go 1.27.3.

## Review limits

- **Fixes:** no fix for B1 or for any major finding was written or tested.
- **B1 variants:** only direct websocket sessions were reproduced. The
  polling-to-websocket upgrade variant was inferred from the code.
- **Platforms:**
  - All runtime testing ran on darwin/arm64.
  - Go 1.22 tests were not run locally; they run in CI only.
  - No Linux, Windows or 32-bit runtime testing was done.
- **Clients:**
  - All JavaScript runs used Node with `forceNode`; no browser was tested.
  - No third-party EIO3 clients (Java, Swift, Python) were tested.
- **Redis:**
  - No live mixed cluster of upstream `v1.7.0` and fork instances was run. M3 rests on code
    reading and on pattern matching against a real Redis server.
  - A hostile Redis peer was not fuzzed.
  - Failover and reconnect under load were not tested.
- **Go client:** reconnect races, ACK-after-disconnect races and sustained websocket load
  were exercised only by the existing suites.
- **Baselines:**
  - There is no end-to-end performance comparison with `v1.4.2`, because its websocket
    broadcast deadlocked in the benchmark harness.
  - `apidiff` was not run against upstream `v1.8.0-rc.1` or upstream `master`.
  - Absolute timings are noisy: the reviews ran concurrently, with load averages up to
    about 130.
- **Security:** only CORS, limits, fuzzing and session IDs were checked. Polling request-rate
  abuse, cookie and proxy deployments, and TLS or proxy header handling were not reviewed.
- **Docs:** PROTOCOL.md wire claims were spot-checked, not verified end to end. How
  pkg.go.dev renders the alias types could not be checked before publication.

## Finding catalogue

Every finding is listed under the dimension that reported it.

- **Severity** shows the reported value and, after the arrow, the value after re-checking.
- **Re-check** shows the outcome for blocker and major findings. Minor and nit findings were
  not re-checked.
- Line numbers refer to `7311fbe`.

### Correctness and security findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| C1 | blocker | confirmed | `engineio/internal/session/session.go:489` | Permanent three-way deadlock between a session's read path, an in-flight websocket write and Close/Abort/Shutdown | Do not hold upgradeLocker while calling into transport deadline setters. In setDeadline, read s.conn under RLock, release the lock, then call SetReadDeadline and SetWriteDeadline, as nextReader and nextWriterCompress already do. Also make Session.fail lock-free: keep closed and conn in atomics or snapshot them without the RWMutex, so a failing writer can never wait on a pending Close. Optionally store the websocket write deadline atomically and apply it inside nextWriter under writeLocker, so SetWriteDeadline does not wait for an in-flight write. Make engineio.Server.Close shut sessions down concurrently or with a bound, so one stuck session cannot block the others. Add a -race regression test: a non-reading websocket client keeps sending pings while the server emits large payloads, and Close/Abort/Server.Close must return promptly with OnDisconnect fired. |
| C2 | major → minor | partially-confirmed | `server.go:406` | Dynamic namespaces created by clients through OfFunc are never removed (unbounded memory growth) | Remove a dynamic child, and close its adapter, when its last socket leaves (the equivalent of socket.io 4.6 cleanupEmptyChildNamespaces), or at least when no socket ever connected. Alternatively, offer a cap or opt-in cleanup option. State the retention in the OfFunc godoc and docs/V1.md. |
| C3 | minor | — | `engineio/server.go:303` | Duplicate session ID from a custom IDGenerator silently replaces a live session; Server.Close leaves the first one open | Under s.lifecycle in newSession, reject the handshake (or ask the generator again) when the sid is already registered or in s.live, and document that IDGenerator must return unique IDs. |
| C4 | minor | — | `namespace_handlers.go:223` | Panics in a user OnError handler are swallowed without any log | Log the recovered value through the connection or server logger, e.g. c.log.Warn("socketio: OnError panic", ...), as is done for other unhandled errors. |
| C5 | minor | — | `engineio/options.go:178` | Permissive CORS default (origin reflected with credentials, websocket origin check disabled) is documented only in godoc | Document the default and how to restrict it (WithCORS(false), transport CheckOrigin) in docs/V1.md in a security section. Consider an example that uses an origin allow-list. |

### API compatibility findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| API-1 | blocker → major | partially-confirmed | `CONTRIBUTING.md:24` | No v1 release notes or migration guide for upstream users; both were deleted just before release | Before tagging, restore a concise migration document owned in one place (for example a 'Migrating from googollee/go-socket.io v1.4–v1.7' section in docs/V1.md, or docs/MIGRATION.md linked from README). It should cover: the import path rewrite (a `replace` drop-in does not work, see API-2); the old-to-new identifier table (API-6/7/8); a default-behavior change table (API-3/4/5); and the Redis rolling-upgrade procedure. Publish the same summary as the GitHub release notes for v1.5.0, and update CONTRIBUTING.md:24-27,56 accordingly. |
| API-2 | major → minor | partially-confirmed | `CONTRIBUTING.md:32` | "v1.5.0" sits on a descendant of upstream v1.7.0 and clashes with upstream's own v1.5.0; the fork path's existing tags are unusable | Either choose a version above every upstream v1 tag (e.g. v1.8.0, or v1.9.0 to avoid confusion with upstream v1.8.0-rc.1), or keep v1.5.0 and state in README and the release notes that fork versions do not continue upstream numbering and that this is a new module with breaking changes. Either way, add `retract [v0.9.1, v1.4.2] // upstream tags; go.mod declares github.com/googollee/go-socket.io` to go.mod in the release commit so `go list -m -versions` and pkg.go.dev stop offering the unusable versions. |
| API-3 | major | confirmed | `engineio/options.go:39` | CORS default silently changed from same-origin to permissive-with-credentials and is undocumented outside godoc | Keep the decision if intended, but document it in docs/PROTOCOL.md (which owns wire behavior) and put it at the top of the migration and release notes, with `engineio.WithCORS(false)` and explicit `CheckOrigin` examples. Consider a short security note in the WithCORS godoc. |
| API-4 | major | confirmed | `adapter/redis/adapter.go:121` | Redis adapter wire format and channel names changed: mixed old/new Go instances lose cross-instance broadcasts during a rolling upgrade, and this is undocumented | Document in PROTOCOL.md or the migration guide that upstream Go Redis peers are wire-incompatible, and give the procedure: use a new `WithPrefix` for the new fleet, or do a stop-the-world upgrade. |
| API-5 | major → minor | partially-confirmed | `socket.go:257` | Other default and behavior changes vs upstream v1.7.0 have no release note; the non-root Conn.ID format is not in public godoc | Add a 'Changed defaults' table (old value, new value, opt-out) to the migration and release notes. State the namespace ID format in the public `Conn.ID` godoc (root types.go/connection contract), not only on the private method. |
| API-6 | minor | — | `server.go:276` | Root package: incompatible API changes vs v1.4.2 and v1.7.0, with no deprecation path | Include this old-to-new table in the migration guide. No compatibility stubs needed. |
| API-7 | minor | — | `engineio/conn.go:8` | engineio: five packages removed; Options/Dialer/Opener/Pauser removed; engineio.Conn gained methods | Cover it in the migration guide. The deleted V1_MIGRATION.md 'Engine.IO codecs and polling' table is a usable starting point once intermediate-master names are replaced with upstream v1.7.0 names. |
| API-8 | minor | — | `server.go:276` | Calls from v1.4.2-era code still compile but change meaning (BroadcastToRoom, parser Encode) | Call out both in the migration guide as 'compiles but changes meaning'. Code changes are optional, e.g. a vet-able note in the BroadcastToRoom godoc that the first argument is the namespace. |
| API-9 | minor | — | `engineio/conn.go:8` | Public aliases to internal packages hide method sets and fields from go doc, contrary to the project's own rule | Before the API freeze, define at least engineio.Conn, CloseError and RemoteError in their public packages (internal code can alias the public type instead), or extend TestPublicRoleDocumentation to these types. |
| API-10 | minor | — | `README.md:29` | README on master advertises an install command and pkg.go.dev links for a v1.5.0 that does not exist yet | Tag promptly after merge, or keep version-less instructions on master until the tag exists. Add the engineio pkg.go.dev link as CONTRIBUTING requires. |
| API-11 | minor | — | `Makefile` | No API-compatibility gate for v1 after release | After tagging, add a CI step such as `apidiff -m -incompatible` (or `gorelease -base=<latest v1 tag>`) against the latest v1 tag for the root module, failing on incompatible changes. |
| API-12 | nit | — | `engineio/parser/fake.go:61` | Test fakes returning unexported types are part of the public engineio/parser API | Move them to an internal test-support package or _test.go files before tagging, or accept and document them. |
| API-13 | nit | — | `go.mod:3` | Go version floor raised to 1.22 (v1.4.2: no directive; v1.7.0: go 1.16) | Already documented in README.md:32; also list it in the release notes. |

### Clarity findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| CL-1 | major | confirmed | `README.md:41` | No minimal quick start anywhere: README, V1.md and godoc never show mounting, Serve, Close or client Connect | Add a 20-30 line server+client quick start to the README or the root package doc: NewServer, OnConnect/OnEvent, `go server.Serve()`, `defer server.Close()`, `http.Handle("/socket.io/", server)`, client.New/Connect/Emit with ack. Add runnable `Example` functions in the root and client packages. State in the Server type doc and Serve doc that Serve must run for connections to be dispatched. |
| CL-2 | major → minor | partially-confirmed | `adapter/redis/adapter.go:153` | Redis adapter silently drops room/namespace broadcasts with a trailing callback, while godoc says callbacks are supported | Either make Send/SendAll log or return the rejection (and have BroadcastToRoom/BroadcastToNamespace return false), or correct the redis Send/SendAll comments, the ErrBroadcastAck doc and the BroadcastToRoom/BroadcastToNamespace docs to say that clustered adapters reject callbacks and send nothing. |
| CL-3 | major → minor | confirmed | `server.go:150` | OnError contract undocumented: nil Conn for root rejections and silently recovered panics; V1.md states the opposite | Document on both OnError methods that Conn is nil for root connect/middleware failures and an inactive Conn for other namespaces, and that panics in OnError are recovered and discarded. Fix the wording at V1.md:41. Consider logging recovered OnError panics at WARN. |
| CL-4 | major | confirmed | `README.md:7` | No migration guide from googollee/go-socket.io despite pervasive API changes; Conn.ID format change is undocumented | Add a short 'Migrating from googollee/go-socket.io' section, in V1.md or a linked file: import path, the constructor/option mapping, Redis adapter setup, client package, ID format, and removed APIs. State the ID format in the Conn.ID godoc of both roles. |
| CL-5 | minor | — | `.golangci.yml:17` | 9 of 12 public packages lack a package comment; lint explicitly disables the checks | Add package docs (root and client with a usage sketch). Document the remaining exported identifiers. Re-enable ST1000/ST1020-1022 or revive's exported rule, at least for the root, client, engineio and adapter packages. |
| CL-6 | minor | — | `adapter/redis/cluster.go:26` | redis.Cluster methods have unnamed parameters; argument order and the bool meaning are invisible | Name the parameters in the interface (ctx, sid, room, closeEngine) and add one-line docs per method, including error and timeout semantics. |
| CL-7 | minor | — | `adapter/adapter.go:66` | adapter.Adapter AllRooms/Rooms comments contradict the implementation | Rewrite the interface method docs to state that AllRooms lists all rooms in the namespace (cluster-wide for Redis) and that Rooms(nil) equals AllRooms, and convert the trailing comments into proper doc comments. |
| CL-8 | minor | — | `server.go:276` | Overlapping server APIs with different semantics and no guidance on which to use | In V1.md, state that the Server.*(namespace, ...) methods are retained upstream-compatible wrappers and that Of(...) / Emitter is the primary API, and document the ack difference. In godoc, cross-reference the two Use variants. |
| CL-9 | minor | — | `_examples/default-http/main.go:19` | Starter example adds an explicit allow-all CheckOrigin that suggests CORS is off by default; the permissive CORS default is not in user docs | State the CORS default and how to restrict it (WithCORS(false) or a transport CheckOrigin) in V1.md. Drop the redundant CheckOrigin from default-http or replace it with a restrictive example. |
| CL-10 | minor | — | `README.md:29` | README advertises an unpublished v1.5.0 on master; inherited googollee tags make @latest fail | Tag v1.5.0 promptly after merge. Consider adding `retract [v0.9.1, v1.4.2] // upstream tags with module path github.com/googollee/go-socket.io` to go.mod in the release commit. Align the CONTRIBUTING step with the actual README links. |
| CL-11 | minor | — | `client/typed.go:152` | Exported 'internal bridge' methods with internal-package parameter types on client.Client and client.Socket | If they must stay exported for the cross-package bridge, group and label them consistently, for example 'Deprecated-style: for socketio.Event use only', and document Socket.RegisterEvent. Otherwise move the bridge behind an unexported interface satisfied via internal/event. |
| CL-12 | minor | — | `engineio/client/client.go:489` | Core concurrency code is hard to follow: 100-219 line functions and no written close/drain state machine | Add a short state-machine comment (or an ARCHITECTURE.md subsection) for connection close phases and client probe/upgrade. Annotate which mutex guards each field. Split probeTransport into dial, probe exchange, pause and commit steps. |
| CL-13 | nit | — | `_examples/default-http/go.mod:1` | Cosmetic inconsistencies in example module paths, error prefixes and option wording | Fix the module path typos, use a consistent package prefix in error strings, say 'nonpositive' where the code means it, mention the Go client in the README tagline, and document or unexport the test fakes. |
| CL-14 | nit | — | `CONTRIBUTING.md:3` | Contributor build/test instructions live in CLAUDE.md, an agent-instruction file | Move the Commands/Interoperability sections to CONTRIBUTING.md, which per the ownership table owns the contribution procedure, and have CLAUDE.md link to it. |

### Architecture findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| ARCH-1 | major | confirmed | `adapter/redis/adapter.go:154` | Error-less adapter Send/SendAll silently drop Redis broadcasts; Server.BroadcastToRoom returns true and godoc promises per-socket callbacks | Before release, log WARN in redis Send/SendAll when SendTo fails, and fix the BroadcastToRoom/BroadcastToNamespace godoc: the bool only means the namespace exists, and callbacks depend on the adapter. Consider documenting Send/SendAll as legacy wrappers over SendTo, with a helper third-party adapters can reuse, before freezing the adapter.Adapter interface. |
| ARCH-2 | minor | — | `engineio/server.go:1` | Public types are aliases to internal packages, hiding their methods from go doc and breaking the project's own rule | Declare engineio.Conn (and the other session-facing types) in the public engineio package with session implementing them. For internal/event, wrap Handler in an opaque public struct without exported methods, or move the types to a public role-neutral package. Extend TestPublicRoleDocumentation to engineio and the root non-role types. |
| ARCH-3 | minor | — | `client/event.go:10` | Typed client events live in the server package, so client-only programs must import the server | Before v1.5.0, consider a public role-neutral package (for example `event`) that owns Event/AckEvent/Endpoint/RemoteError and the sentinel errors, with root aliases kept for convenience, so client.RegisterEvent no longer exposes an internal type. |
| ARCH-4 | minor | — | `queue.go:88` | Vestigial non-server mode in the server connection: boundary is always true and the bounded writeChan is never used | Remove `boundary` and the writeChan path. Keep an integer capacity and a single ordered queue, and simplify the budget expressions accordingly. |
| ARCH-5 | minor | — | `adapter/adapter.go:46` | Adapter extension contract is redundant and partly duck-typed | Before tagging, fix the AllRooms comment and declare the optional capabilities as named interfaces in package adapter (e.g. `SocketLookupBinder`). Document that the Adapter method set is frozen for v1 and new capabilities arrive as optional interfaces. |
| ARCH-6 | minor | — | `types.go:61` | Conn.EngineConn exposes full engine frame I/O alongside the library's own reader and writer | Document EngineConn as metadata-only (ID, Transport, Upgraded, ReadyState, Close), or return a narrower public interface before v1.5.0. |
| ARCH-7 | minor | — | `client/namespace_handlers.go:100` | Server and client runtimes are maintained in parallel | Keep role independence, but extract the role-neutral pieces (listener registry, frame failure tracking, header-reader worker) into an internal package the graph allows for both roles, or add paired behavioral tests that run against both roles. |
| ARCH-8 | minor | — | `engineio/client/client.go:489` | Complexity hotspots in lifecycle code, including a busy-wait caused by a non-cancellable Pauser | For v1.5.0 no restructuring is required, but freeze behavior with the existing tests. Afterwards, give Pause a cancellation path (a context or stop channel) and model the connection close as an explicit state enum. |
| ARCH-9 | minor | — | `namespace.go:161` | Dynamic child namespaces are never removed (unbounded registry growth), and OfFunc does not document it | Document the retention in OfFunc and in docs/V1.md, and advise restrictive match functions. Consider a later option to remove empty children (as socket.io 4.6 cleanupEmptyChildNamespaces does). |
| ARCH-10 | minor | — | `parser/encoder.go:138` | Parser reimplements encoding/json traversal for every outbound payload; the GOEXPERIMENT=jsonv2 build variants are not exercised in CI | Add a CI job with GOEXPERIMENT=jsonv2 on the newest Go, and keep the json_compatibility tests as a differential check against encoding/json. Consider a fast path that uses json.Marshal directly when the value contains no parser.Buffer. |
| ARCH-11 | nit | — | `engineio/parser/fake.go:61` | engineio packages export test fakes and other API oddities | Move the fakes to an internal test helper package, unexport TimestampFromClock, and fix the stale doc comment before tagging. |
| ARCH-12 | nit | — | `adapter/redis/cluster.go:104` | Redis log records use the attribute key 'namespace', which the logger contract does not list | Use nspAttr-style `nsp` with the '/' convention. |
| ARCH-13 | nit | — | `docs/ARCHITECTURE.md:36` | ARCHITECTURE.md runtime section undercounts per-connection goroutines | List all five Socket.IO workers, and say that blocking a handler also blocks the header worker handoff. |

### Performance findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| PERF-1 | major | confirmed | `parser/json_encode.go:114` | Branch's reflective JSON encoder regresses encode cost vs master and v1.4.2 (per-leaf json.Marshal, per-packet map, extra copies) | Keep the visibility and snapshot semantics but add a cached per-reflect.Type fast path. When a type's subtree cannot contain a Buffer, an interface or a custom marshaler, encode the whole subtree with one json.Marshal or Encoder call. Write string and number leaves with append-style helpers instead of json.Marshal per leaf. Create the cycle-detection map lazily, only past a depth threshold as encoding/json does. Write directly into the final buffer with header space reserved, avoiding the copy into `data`. Add a parser encode benchmark before merging. |
| PERF-2 | major → minor | partially-confirmed | `engineio/transport/polling/payload.go:412` | Polling server creates a long time.After timer per packet; under go1.22 module semantics they keep heap memory alive for ~45s | Use one per-payload timer for each direction and Reset/Stop it, or an AfterFunc/context set in SetRead/WriteDeadline, and stop the timer when select returns on another case. Add a test that asserts bounded heap after N polling packets with asynctimerchan=1. |
| PERF-3 | major | confirmed | `adapter/redis/codec.go:191` | Redis PUBLISH has no I/O deadline; a stalled Redis blocks every broadcasting caller indefinitely | Bound publishing I/O: add DialReadTimeout and DialWriteTimeout (for example the requests timeout), or use redis.DoContext with a deadline. Alternatively, publish through a bounded asynchronous queue and report errors through the adapter error path. |
| PERF-4 | minor | — | `adapter/redis/adapter.go:100` | Redis publishing pool (MaxIdle 4, unbounded MaxActive) churns TCP connections under concurrent broadcasts | Size MaxIdle to the expected publish concurrency (or expose an option). Alternatively set MaxActive with Wait: true so callers queue on a bounded set of connections. |
| PERF-5 | minor | — | `engineio/transport/polling/transport.go:24` | Go polling client: polling.Default shares http.DefaultTransport (2 idle conns/host), so multiple clients per process churn TCP and exhaust ephemeral ports | Give polling.Default its own http.Transport with a larger MaxIdleConnsPerHost (for example 64), or create a transport per dialed client. Document WithTransports(&polling.Transport{Client: ...}) for high client counts. |
| PERF-6 | minor | — | `adapter/memory.go:139` | Broadcast encodes the packet once per recipient; the shareable PreparedPacket is not used for fan-out | Add an internal fast path: prepare once in the broadcast path and have namespaceConn accept a prepared packet, for example through an unexported optional interface checked by Memory and Redis, without changing adapter.Socket. Per-socket ACK variants already use WithHeader. |
| PERF-7 | minor | — | `socket.go:270` | Every send copies the full packet via WithHeader even when no ACK ID was added | Call WithHeader only when registerAck changed the header (ack != nil). |
| PERF-8 | minor | — | `engineio/transport/websocket/wrapper.go:146` | Websocket wrapper spawns a goroutine and a 30s timer for every frame read and written | Drop the nag, or make it debug-only. If kept, use time.AfterFunc with no goroutine and stop it on Close. |
| PERF-9 | minor | — | `server.go:252` | Server.JoinRoom/LeaveRoom/LeaveAllRooms copy and scan all namespace sockets on each call | Check membership with a map lookup under connectedMu (nh.connected[nc.ID()] == nc) instead of copying the slice. |
| PERF-10 | minor | — | `.github/workflows/benchmarks.yml:148` | No Socket.IO-level benchmarks; CI benchmark comparison cannot see parser/server regressions | Add benchmarks for parser Prepare/Encode/Decode (string, struct, struct slice, binary), memory-adapter broadcast to N fake sockets, and an emit+ACK round trip over httptest websocket. Then let the existing base-vs-PR workflow pick them up. |
| PERF-11 | minor | — | `parser/decoder.go:87` | Inbound decode 42% slower than v1.4.2; allocates a 4 KiB bufio.Reader per packet and parses JSON three times | Reuse a bufio.Reader per Decoder (Reset), or read directly when the frame reader is already a byteReader. Fold json.Valid into the RawMessage unmarshal, which already validates. |
| PERF-12 | nit | — | `adapter/memory.go:107` | Broadcast target selection builds a dedup map of all members even for one room without Except | Skip the seen map when len(opts.Rooms) <= 1 and len(opts.Except) == 0, and pre-size the result slice. |
| PERF-13 | nit | — | `connection.go:281` | 7 server goroutines per idle websocket connection (v1.4.2: 4) | Optional: merge the engine close watcher with observeEngineClose, and start the error loop lazily. |

### Interoperability findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| interop-1 | major → minor | confirmed | `engineio/transport/websocket/wrapper.go:39` | WebSocket transport ignores Engine.IO base64 mode; a forceBase64 client is disconnected when it sends binary | In the websocket transport, decode text frames that start with 'b' as base64 binary packets. When the request carries `b64=1`, write binary packets as `b<base64>` text frames, as engine.io 3.6.2 does. Add a forceBase64 case (websocket and upgrade) to testdata/node/client.js. If this is not implemented for v1.5.0, document it in PROTOCOL.md as unsupported. |
| interop-2 | minor | — | `parser/encoder.go:144` | Every JSON payload and the OPEN packet carry an undocumented trailing newline | Either drop the newline (both the Socket.IO payload and the OPEN packet), or record it in PROTOCOL.md as an intentional difference kept for compatibility with older go-socket.io releases. |
| interop-3 | minor | — | `engineio/transport/polling/util.go:21` | Polling POST rejects Content-Types that the reference server accepts | Match the reference: treat any non-octet-stream body as text. Otherwise, document the exact accepted media types as a Go-specific restriction in PROTOCOL.md. |
| interop-4 | minor | — | `packets.go:211` | Connect and event middleware rejections are always sent as a string; no counterpart to Node's err.data | Allow an error type (for example one with `Data() any`) whose value becomes the ERROR payload, or document in PROTOCOL.md that ERROR payloads are always `err.Error()` strings. |
| interop-5 | minor | — | `packets.go:103` | Untyped handler with a mismatched argument type sends no ACK, so the JS callback never fires (undocumented) | Document the behavior next to the ACK rules in V1.md or PROTOCOL.md. Consider sending an error ACK or an ERROR packet so JS callers can detect the failure. |
| interop-6 | nit | — | `engineio/transport/websocket/connect.go:117` | Server-initiated websocket close sends no close frame | Write a CloseMessage (1000) with a short deadline before closing the underlying connection on orderly server closes. |
| interop-7 | nit | — | `docs/PARITY.md:19` | PARITY.md lists heartbeat as covered by raw conformance tests, but no conformance test pings | Add a short-interval heartbeat scenario (for example `WithPingInterval(200ms)`, then idle for about 1 s and send an ACK) to client.js for both transports, or correct the PARITY.md row. |
| interop-8 | nit | — | `internal/conformance/testdata/node/server/redis-cluster.js:199` | Redis mixed-cluster polling scenario idles 30 s after finishing, leaving little margin in runNode's 45 s timeout | End the scenario with an explicit `process.exit(process.exitCode \|\| 0)` after cleanup, or close the clients before `io.close()` so the reference server's polling transports do not wait out closeTimeout. |
| interop-9 | nit | — | `docs/PROTOCOL.md:60` | Unknown Socket.IO packet type and missing CORS wire description are undocumented differences | Add a CORS bullet (default, WithCORS(false), preflight) and a note on how invalid packets are handled to PROTOCOL.md. |

### Tests findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| T1 | minor | — | `parser/json_encode.go:252` | omitempty handling in the rewritten Socket.IO JSON encoder has zero test coverage | Add a table-driven differential test in parser/json_compatibility_test.go that encodes omitempty structs (all kinds handled by isJSONEmpty, including nested and Buffer-bearing fields) and compares against json.Marshal. Consider adding an encoder fuzz target that compares against encoding/json for binary-free values. |
| T2 | minor | — | `engineio/transport/websocket/wrapper.go:76` | No goroutine-leak detection; a probe finds library frame-reader goroutines left for 30s after parse/mid-frame errors, plus leaking client test fixtures | Close the last frame reader on the decode-error and transport-failure paths in the server and client read loops. Fix the leaking fixtures by closing clients via t.Cleanup and using buffered or select-guarded channels in handlers. Add a goroutine-leak check (go.uber.org/goleak VerifyTestMain or an equivalent NumGoroutine wait) to the root, client and engineio packages so close and shutdown regressions fail CI. |
| T3 | minor | — | `docs/PARITY.md:20` | Engine.IO periodic heartbeat is never exercised against a real JS client, contrary to PARITY.md | Add a conformance scenario with WithPingInterval(200ms) and WithPingTimeout(200ms) that keeps a JS client connected across several intervals on each transport. Also have the client suppress pings and assert 'ping timeout'. Alternatively, correct the PARITY.md row. |
| T4 | minor | — | `redis_integration_test.go:174` | TestRedisServerCloseStopsBroadcasts spends 10s inside a blocking OnConnect and neither bounds nor documents that behavior | Expose a test hook or redis option (dial timeout) so the root test can use a short timeout and assert that registration returns within it. Document in docs/V1.md that registering a handler on a new namespace dials Redis synchronously and can block up to the dial timeout. |
| T5 | nit | — | `parser/prepared_test.go:64` | Fuzzing covers only decode/discard paths; no encoder or round-trip fuzz, and Engine.IO packet decoder has no own tests | Add FuzzDecodeArgs (decode into a fixed set of typed targets) and a round-trip fuzz (Prepare -> Decoder -> compare) for binary-free values. |
| T6 | nit | — | `engineio/transport/polling/pauser_test.go:206` | Residual sleep-based synchronization in inherited polling tests weakens rather than flakes | Replace the sleeps with explicit readiness signals, for example a hook or channel signalled when the goroutine enters the blocking wait. |

### Release engineering findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| REL-1 | major → minor | partially-confirmed | `CONTRIBUTING.md:53` | No v1.5.0 release notes or migration guide, although this release breaks the upstream API | Before tagging, write release notes for the GitHub release covering the module path change, the API mapping from upstream v1.7.0/v1.8.0-rc.1, changed defaults and the support matrix. Either add a short 'Migrating from googollee/go-socket.io' section to docs/V1.md or link one from the release. Add a 'publish GitHub release notes' step to CONTRIBUTING 'Before tagging v1'. |
| REL-2 | major → minor | partially-confirmed | `CONTRIBUTING.md:49` | A v1.5.0 tag was published and deleted before; the stale module is still cached and will undermine release verification | Before tagging, delete the cached v1.5.0 entries (cache/download/.../@v/v1.5.0.* and the extracted @v1.5.0 directory, or `go clean -modcache`). Run the consumption check with a fresh `GOMODCACHE=$(mktemp -d) GOFLAGS=-mod=mod go get github.com/sshaplygin/go-socket.io@v1.5.0`. Alternatively, choose a version that was never tagged. |
| REL-3 | minor | — | `.git/config` | The repo-local git identity is a placeholder and has leaked into master history; an annotated tag would carry it | Remove or replace the local user.name/user.email before creating the tag or merging more PRs. Check the squash-merge body for Co-authored-by lines before confirming. |
| REL-4 | minor | — | `.github/workflows/ci.yaml:378` | The full CI on push and schedule never runs the stress job, although the docs say it runs everything | Run stress-v1 on push and schedule with PKGS=./..., or add a manual `make test-stress` step to the release checklist. Make CLAUDE.md and the ci.yaml comment match whichever you choose. |
| REL-5 | minor | — | `.github/workflows/ci.yaml:17` | Branch protection requires only the drift and conformance checks; the v1 test, lint and gate jobs are not required | Add Lint v1, Gates v1, the Test v1 matrix and the Go 1.22 job to the required checks; they report even when skipped by their job-level if. Or add an aggregating 'CI ok' job and require that. |
| REL-6 | minor | — | `README.md:29` | README on master advertises v1.5.0 before it exists; the release checklist's Engine.IO link is missing; old upstream tags make @latest fail today | Push the tag promptly or keep the versioned links in the release PR, as CONTRIBUTING intends. Add the engineio pkg.go.dev link. Consider adding `retract [v0.9.1, v1.4.2]` to go.mod in the release commit to hide the inherited upstream tags. |
| REL-7 | minor | — | `CONTRIBUTING.md:32` | Version v1.5.0 reuses an upstream release number and is lower than the upstream versions it descends from | Either confirm v1.5.0 deliberately and say in the release notes which upstream state it supersedes, or choose a number above upstream (for example v1.9.0). This is the owner's decision and must be made before the tag. |
| REL-8 | nit | — | `client/connection_typed_test.go:66` | The test package fails to compile on 32-bit platforms | Use int64 in the assertion (`event.Value[int64]`), or put the case behind a 64-bit build constraint. |
| REL-9 | nit | — | `.github/workflows/ci.yaml:244` | CI tool versions are unpinned and some actions target the deprecated Node 20 | Pin the golangci-lint version, matching the local v2 toolchain, and update to Node-24 action versions. Keep govulncheck@latest only if that is intended. |
| REL-10 | nit | — | `_examples/gf/go.mod:24` | Example modules pin old golang.org/x/net and have typos in their module paths | Bump the framework and x/net versions in the example modules and fix the module path typos. |
| REL-11 | nit | — | — | Stale remote branches, including a superseded release branch and a changelog draft | Delete merged or superseded branches, release/v1.5.0 in particular, before tagging. Reuse the useful parts of the v1-h/release-notes draft for REL-1. |
| REL-12 | nit | — | `engineio/transport/websocket/connect.go:99` | An open TODO remains in library code | Replace the TODO with a comment that explains why the lock is needed. |
