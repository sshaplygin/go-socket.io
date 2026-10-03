# Roadmap

Scope approved: 2026-09-28. Updated: 2026-10-03. Owner: Sam Shaplygin.

This file owns scope, dependencies, implementation contracts and release gates.
Current implementation: [PROTOCOL.md](PROTOCOL.md). Completed changes:
[CHANGELOG.md](../CHANGELOG.md). Development and plan-review workflow:
[CLAUDE.md](../CLAUDE.md).

## Baseline

The starting fork of `googollee/go-socket.io` supports Socket.IO protocol v4 over
Engine.IO v3. Stage 0, toolchain/CI work and logger tasks 1.2/1.2a have landed;
remaining work starts at stage 1 below. Existing application APIs stay on branch
`v1`; v2 is a new core/API in this repository.

Preparation exists on `codex/eio4-payload`, inspected at
`ee682282997b19309036e9c6fef6e248e06bc230`; it is not merged into this baseline.
Stage 2.1 owns its reuse and remaining integration work below.

## Decisions

| Area | Decision | Contract owner |
| --- | --- | --- |
| Core | Own Engine.IO/Socket.IO core; no dependency on or rebase onto `zishang520/socket.io` | 2.0–2.3 |
| Protocol | v2 supports Engine.IO v4 / Socket.IO protocol v5; old clients stay on `v1` | 2.1, 2.3 |
| API | Generic `Event[T]` / `AckEvent[T, R]` from the first v2 implementation; explicit raw escape hatch; no reflection-based dispatch | 2.0, 2.3 |
| Modules | v1 root is `github.com/sshaplygin/go-socket.io`, independent of the upstream module; v2 root adds `/v2`; adapters and contrib have separate modules | 2.5, 4b, 5 |
| Go | Go 1.22 minimum for runtime modules; compatible dependencies pinned and minimum tested; build tools may use stable Go | Stage 1 DoD, 2.5 |
| Transport | `gobwas/ws` + `wsutil` on server and client; standard `http.Handler` integration | 2.1 |
| Brokers | Redis `go-redis/v9`, Node non-sharded adapter wire compatibility; NATS core pub/sub, no JetStream | 4b |
| Logging | Application `slog.Handler` through an injected logger; instance routing, process-wide level override | 2.4 |
| Observability | Nil-able hooks and logging in root; OTel bridge in `contrib/otel`; no OTel dependency in root | 2.4 |
| Admin UI | Required final product stage, separate `contrib/admin`, unchanged official UI; commands disabled by default | 5 |
| Benchmarks | Final comparative campaign after all product features: our v2, existing Go and official JS/TS implementations | 6 |
| Documentation | English; each contract has one owner; other sections refer to it | CLAUDE.md |

## Execution and parallel work

Execution order: **1 → 1b → 2 → 3 → 4b → 5 → 6**. Admin UI remains the last
product stage and starts after M5; the final benchmark campaign starts after M6.
Numbers identify scope, not permission to start before a dependency passes.
A prerequisite marked as a gate means its tests and integration must pass, not only
that a draft API exists. Tasks in the same row may run concurrently in separate
worktrees. One integrator owns shared API files, module manifests and CI workflows;
workers submit changes to these files through that integrator.

| Wave | Prerequisites | Independent work / write ownership | Join gate |
| --- | --- | --- | --- |
| 1A | landed infrastructure | 1.R Redis internals (`redis_broadcast.go`); 1.B queue and close internals (`connection.go`, `broadcast.go`, `errors.go`, the socket.io goroutines and close paths in `server.go` (`serveConn`, `serveRead`, `serveWrite`, `serveError`) and `client.go` (`Connect`, `Close`, `clientRead`, `clientWrite`, `clientError`), and the disconnect handlers in `connection_handlers.go`); 1.S session/server fixes (`engineio/session`, `engineio/server.go`, `server.go`; landed) | component regression tests pass |
| 1I | 1A | integrator wires Redis construction errors through `namespace_handler.go` and `server.go`; wires `WriteBufferSize` and the drain deadline (`PingTimeout`) through `engineio/server_options.go`, `server.go` and `client.go`; runs the 1.B slow-client test against the Redis broadcast | integrated bug tests (including the 1I wiring tests named in 1.B) and root build pass |
| 1B | 1I | 1.L logging call sites across layers; 1.D docs/links in Markdown | M1 checks and v1 compatibility |
| 1b | M1, branch `v1` cut | one refactor owner; moves/merges applied sequentially | M1b regression checks |
| 2A | M1b | 2.0 owner removes legacy root runtime/adapter consumers atomically with the new API skeleton, builds compile fixtures and freezes shared interfaces | G2: fixtures compile, package graph acyclic, no unresolved API signatures |
| 2B | G2 | 2.1 Engine.IO (`engineio/`); 2.2 memory adapter (root `adapter.go`); 2.3P Socket.IO codec (`parser/`) | all three integrate against frozen contracts |
| 2C | 2B | 2.3S server/namespace runtime (root socket files); 2.3C client (`client/`) | typed Go/Node tests and lifecycle tests pass; dispatch baseline recorded |
| 2D | 2C | one owner propagates instance loggers across runtime packages | logger precedence/isolation tests pass |
| 2E | 2D | 2.4E Engine.IO hook fire points; 2.4S Socket.IO hook fire points; 2.4O OTel bridge (`contrib/otel`) against frozen hook fixtures | all hook, span, metric and overhead checks pass |
| 2F | 2E | 2.5T conformance/framework tests; 2.5D migration/examples/docs | M3 pre-release gate, then publication verification |
| 3A | M3 | freeze chat event schema; then server, browser/CLI and load client in separate directories | M4 single-server acceptance |
| 4A | M4 | freeze codec fixtures and adaptertest cases; then Redis and NATS modules independently | each passes shared conformance suite |
| 4B | 4A | cluster chat profile; mixed Go/Node Redis tests in separate test directories | M5 cluster acceptance |
| 5A | M5 | freeze admin snapshot/capability and command-result contracts, and pinned UI fixtures | admin contract gate |
| 5B | 5A | core observers/snapshots; admin wire/auth module; Redis/NATS capability extensions; browser tests against fixtures | integrate and run M6 acceptance |
| 6A | M6 | benchmark owner freezes versions, workload matrix, resource budgets and result schema | comparison contract and correctness checks pass |
| 6B | 6A | our-v2, existing-Go and official-Node runners in separate directories; shared load generator owned by integrator | runners produce equivalent traffic/results |
| 6C | 6B | measurements sequentially on reserved hosts; analysis/report follows complete raw results | M7 reproducibility and report acceptance |

Mocks permit development against frozen contracts; they do not satisfy integration
or release gates. A contract change updates its owning section and fixtures before
consumers continue. Every join builds/tests the whole root module and affected child
modules; re-run affected gates after merges. Each work unit supplies its gate evidence.
Apply the three-agent validation workflow in CLAUDE.md after plan edits; do not
replace unresolved findings with optimistic estimates.

## Stage 0. Documentation baseline

Landed. Maintain the documentation ownership map in CLAUDE.md; protocol facts,
release history and implementation commands stay in their respective files.

## Stage 1. Infrastructure and known bugs (tag `v1.5.0`)

No protocol changes. Allowed API changes are `engineio.Options.Logger`,
`engineio.Options.WriteBufferSize` (temporary v1 placement), `socketio.ErrWriteBufferFull`,
the logger exports
listed in the changelog, and the already-landed session logger parameter. That
session constructor signature change must be called out in v1 migration notes;
root `NewServer` and handler signatures stay unchanged. `gorilla/websocket` stays in v1; the
transport swap happens in stage 2 where the transport is rewritten.

Tasks:

- **1.R Redis:** protect `requests` and room access; time out peer queries; propagate
  adapter construction errors through the 1I integration step; reconnect subscriptions with backoff after receive
  failures. Each fix has a regression test, including two-server tests under `-race`.
- **1.B Backpressure:** each connection has a bounded queue of outbound packets. The
  rules below apply to `Server` connections and to `Client` alike.
  - *Size:* temporary v1 `engineio.Options.WriteBufferSize` counts socket.io packets
    (unrelated to `websocket.Transport.WriteBufferSize`, which counts bytes); 0 and
    negative values mean the default 64; no opt-out in v1. A packet the writer has
    started (see *Overflow*) no longer counts toward it. 1.B uses an unexported
    default; 1I wires the option.
  - *Drain deadline:* `engineio.Options.PingTimeout` as passed to `NewServer` /
    `NewClient`; nil options, 0 and negative values mean one minute. It is read by the
    socket.io layer without changing the `engineio.Conn` interface. For `Client` it is
    the local option, not the server's handshake value. 1.B keeps it in an unexported
    per-connection field (tests set it); 1I wires it.
  - *Emit* never blocks. Emits from `OnConnect` are queued and written once the writer
    starts. Packets the library queues itself (ACK replies, namespace CONNECT replies)
    follow the same rules as `Emit`, so the read goroutine never blocks on a full queue.
  - *Connected namespace:* a namespace counts as connected from the moment it is
    registered, before its `OnConnect` runs, as in v1.4. A namespace whose `OnConnect`
    failed therefore gets its one `OnDisconnect` when the connection closes.
  - *First close decides:* a connection is closed at most once. The first close fixes
    which `OnDisconnect` calls run and whether Emits still queue; `OnDisconnect` runs
    exactly once per connected namespace whatever the number and kind of closes, also
    when a peer namespace DISCONNECT is being dispatched as the close starts; later
    `Close` calls return `nil` and change nothing. A later library-started close can
    only shorten a drain: any read or writer failure during a drain, whatever its
    cause, ends the drain at once and discards the rest, without a report. Once any
    close has started, an `Emit` is dropped silently, never blocks and is not reported,
    except that a draining close still queues packets until its seal.
  - *Overflow* (no close started): an Emit that finds the queue full marks the
    connection overflowed, drops that packet and every later one, and starts a
    discarding close without blocking the emitter. `OnDisconnect`, leaving the rooms
    and one `ErrWriteBufferFull` report (exported sentinel) run on the connection's
    own goroutines. An overflow before those goroutines start always takes the
    connect-failure path, whatever `OnConnect` returned; there `serveConn` runs them
    synchronously and passes a nil `Conn` to the report, as v1.4 does for connect
    failures, and an `OnConnect` error is reported separately. The report goes to
    `OnError` of the namespace of the overflowing packet and is delivered although the
    connection is closing; with
    no `OnError` registered it is dropped (1.L logs it). Report and `OnDisconnect` have
    no guaranteed order. After the overflow flag is set the socket.io writer starts no
    further packet. A packet has started once the writer called `NextWriter` for its
    first frame; the overflow's engine.io close makes a blocked `NextWriter` fail. A
    started packet counts as in flight together with its binary attachments.
    Engine.io control frames are out of scope.
  - *Draining close:* only `Conn.Close` or `Client.Close` called by application code
    (including from handlers) drains. `Close` runs `OnDisconnect`; the queue is
    *sealed* when every `OnDisconnect` called by this `Close` has returned. Packets
    queued before the seal, including Emits from `OnDisconnect`, are written by the
    writer goroutine in the background; an Emit that finds the queue full before the
    seal is dropped silently and not reported. The drain ends when the queue is empty
    after the seal, or at the drain deadline measured from the start of `Close`,
    whichever is first; remaining packets are discarded and the engine.io connection
    is closed. From the start of `Close` the read goroutine keeps reading, so engine.io
    pings are answered and a peer close is detected, but it dispatches no CONNECT,
    EVENT, ACK or DISCONNECT packet. `Close` returns `nil` without waiting. A `Close`
    from root `OnConnect` that then returns nil is not a connect failure: the
    goroutines start and the drain runs.
  - *Discarding closes:* every close started inside the library discards the queue and
    closes the engine.io connection at once: header read or decode error, argument
    decode error, dispatch error, encode error (any `Encode` failure, marshal or
    transport write; in v1.4 the connection stayed open), peer close (engine.io CLOSE
    or the transport closed or failed from the peer side), ping timeout, overflow, and
    a connect failure before the writer starts. Reports are routed as in v1.4: a
    failure to read or decode a packet header goes to root `OnError`, and so do a peer
    close and a ping timeout, which reach the reader as such a failure; an argument
    decode, dispatch or encode error goes to `OnError` of the packet's namespace; a
    connect failure goes exactly once to root `OnError` with a nil `Conn`, whether it
    came from `Encode` or from `OnConnect`. A report of the close's own cause comes
    first: the close starts only after its `OnError` call returns, so Emits from that
    call still queue and can overflow. On the connect-failure path, where an overflow
    may already have started the close, `serveConn` delivers the connect-failure
    report, then the overflow report if any, and only then discards the queue, runs
    `OnDisconnect` and closes engine.io (v1.4 closed first and reported after). Once any
    close has started, a read or writer failure, including one caused by the library's
    own engine.io close, is not reported and starts no new close; during a drain it
    ends the drain (see *First close decides*). v1 `Server.Close` does not close
    sessions.
  - *Namespace DISCONNECT:* a socket.io DISCONNECT from the peer ends only that
    namespace: its `OnDisconnect` runs once and its rooms are left; the engine.io
    connection and the queue stay as they are.
  - *Docs:* godoc of `WriteBufferSize`, `Emit` and `Close` states the rules above;
    that more than `WriteBufferSize` packets queued faster than the writer drains them
    *can* close a healthy client; that polling writes one engine.io frame per poll
    round trip (a packet with k binary attachments needs k+1), so polling clients
    overflow at much lower emit rates; that an encode error now closes the connection;
    that a closing error is now reported before the close's effects run; and that after `Close` returns the transport closes asynchronously, so
    `Server.Count` still counts the session until then. The changelog states the
    changes and links to that godoc.
  - *Broadcasts:* `Send`, `SendAll` and `ForEach` emit or call back after releasing
    the room lock. The Redis broadcast already does this (1.R); 1.B changes the
    in-memory one. One copy per socket for `SendAll` is owned by 2.2; 1.B points the
    pinning comment in `lifecycle_test.go` at 2.2 only.
  - *Tests (1.B):* deterministic under `-race` with a fake `engineio.Conn` whose
    writer can be blocked; 1.B adds an unexported dial seam to `Client` so the `Client`
    cases use the same fake. Every close case also asserts the `OnDisconnect` call
    count. Side: S = `Server`, C = `Client` (root namespace only).
    1. 1B-T1 (S): one stalled member does not block another member of its room.
    2. 1B-T2 (S, C): a draining `Close` delivers N queued packets, N = 1 and N = 64,
       and returns while the writer is still blocked.
    3. 1B-T3 (S, C): a draining `Close` writes Emits from `OnDisconnect`, including one
       made after the writer emptied the queue.
    4. 1B-T4 (S, C): an Emit after the seal and an Emit that finds the queue full
       during `OnDisconnect` are dropped without report.
    5. 1B-T5 (S, C): after a draining `Close` an incoming EVENT runs no handler and an
       incoming CONNECT runs no `OnConnect`.
    6. 1B-T6 (S, C): with a 50 ms drain deadline a stalled peer's engine.io connection
       closes within 50 ms + 1 s, and `OnError` is not called.
    7. 1B-T7 (S, C): a read-error close discards and closes at once.
    8. 1B-T8 (S, C): an engine.io-CLOSE close discards, closes at once and is reported
       exactly once to root `OnError`.
    9. 1B-T9 (S, C): a dispatch-error close discards and closes at once; on S, an
       argument decode error in a non-root namespace is reported to that namespace's
       `OnError` and not to root.
    10. 1B-T10 (S, C): an encode error is reported once to `OnError` of the packet's
        namespace, then the connection discards and closes at once.
    11. 1B-T11 (S, C): a connect failure discards, closes at once and is reported
        exactly once, before `OnDisconnect` runs. On C the trigger is the CONNECT
        `Encode` failure and nothing can be queued; a Client `OnConnect` error is a
        dispatch error (1B-T9).
    12. 1B-T12 (S, C): a read-error close whose `OnDisconnect` emits
        `WriteBufferSize`+1 packets reports no `ErrWriteBufferFull` and nothing for
        the dropped Emits.
    13. 1B-T13 (S, C): a read failure from the fake reader during a draining `Close`
        ends the drain at once, unreported.
    14. 1B-T14 (S, C): a peer namespace DISCONNECT whose `OnDisconnect` is held on a
        channel races `Close`; `OnDisconnect` runs once for that namespace and once for
        each other connected namespace.
    15. 1B-T15 (S, C): with a packet in flight (its `NextWriter` called and blocked),
        an overflow reports `ErrWriteBufferFull` exactly once, to the
        namespace of the overflowing packet.
    16. 1B-T16 (S, C): with a binary packet in flight (its `NextWriter` called and
        blocked) an overflow lets the writer start no further packet.
    17. 1B-T17 (S): an overflow inside root `OnConnect` returning nil takes the
        connect-failure path, reports `ErrWriteBufferFull` once and writes nothing
        after it.
    18. 1B-T18 (S): the same with `OnConnect` returning an error; `ErrWriteBufferFull`
        and the `OnConnect` error are each reported once.
    19. 1B-T19 (S): an overflow triggered inside `BroadcastToRoom` does not deadlock.
    20. 1B-T20 (S): an overflow triggered inside `ForEach` does not deadlock.
    21. 1B-T21 (S, C): with a packet in flight (its `NextWriter` called and blocked),
        a dispatch error whose `OnError`
        emits `WriteBufferSize`+1 packets does not deadlock; `ErrWriteBufferFull` is
        reported once and `OnDisconnect` runs once.
    22. 1B-T22 (S, C): `Close` from `OnError` does not deadlock.
    23. 1B-T23 (S, C): `Close` from `OnDisconnect` does not deadlock.
    24. 1B-T24 (S, C): `Close` called twice concurrently does not deadlock.
    25. 1B-T25 (S, C): a namespace DISCONNECT keeps the session open; on S the other
        namespaces keep working.
    26. 1B-T26 (S): a draining `Close` from root `OnConnect` returning nil delivers the
        Emits queued before the seal and closes the engine.io connection within the
        drain deadline.
    27. 1B-T27 (S, C): with a packet in flight, an ACK reply that finds the queue full
        starts an overflow close
        and the read goroutine does not block; on S, the same for a namespace CONNECT
        reply.
  - *Tests (1I):*
    1. 1I-T1 (S): 1B-T1 against the Redis broadcast.
    2. 1I-T2 (S, C): `WriteBufferSize` 0 and negative give 64.
    3. 1I-T3 (S, C): a custom `WriteBufferSize` is used.
    4. 1I-T4 (S, C): nil options and `PingTimeout` 0 give a one-minute drain deadline,
       checked on the stored per-connection value.
    5. 1I-T5 (S, C): a negative `PingTimeout` gives one minute, checked on the stored
       per-connection value (engine.io sessions with a negative timeout expire at once).
    6. 1I-T6 (S, C): a custom `PingTimeout` becomes the drain deadline, checked on the
       stored per-connection value. The live bound is 1B-T6: on a real session the
       engine.io write deadline equals `PingTimeout`, so a live test could not tell the
       two apart.
  - *Gate record:* a test covering a case names it in its doc comment, for example
    `// Covers 1B-T3 (S, C).`; the stage 1 DoD checks that every (case, side) pair of
    both lists is named by a passing test.
- **1.S Runtime fixes (landed; its lifecycle tests stay a stage gate):** synchronous session registration before a second request can
  use its SID; `Manager.Count` uses `RLock`; correct EOF result from `Server.Serve`.
  Cover session lifecycle and root connect/event/ack/namespace/room/disconnect paths.
- **1.L Logging:** replace remaining `fmt.Printf`/`log.Print` library calls, normalize
  constant messages and `err`/`nsp` attributes, deprecate nil-safe `logger.Error/Info`.
  Expected closure is DEBUG; swallowed failures are WARN. Emit the boundary records
  below; one session-close record includes reason/duration. Existing logger level,
  fallback and wrapping behaviour is specified once in 2.4. Lower layers using the
  global fallback remain a v1 limitation. TRACE may include up to 256 payload bytes
  in v1; DEBUG never includes payloads.
- **1.D Docs:** reduce `engineio/README.md` to purpose and root/protocol links, register
  it in the ownership map, remove `godoc.org` links, point CI badges at this fork.
  Keep API links on the fork's v1 module until 2.5.

Boundary lines. The keys are a contract reused by stage 2.4.

| Layer | Message | Level | Keys |
| --- | --- | --- | --- |
| engineio | `engineio: request rejected` | WARN | `transport`, `remote_addr`, `reason`, `err` |
| engineio | `engineio: session open` | DEBUG | `sid`, `transport`, `remote_addr` |
| engineio | `engineio: session close` | DEBUG | `sid`, `transport`, `reason`, `duration`, `err` |
| engineio | `engineio: upgrade start`, `engineio: upgrade end` | DEBUG | `sid`, `from`, `to`, `err` |
| engineio | `engineio: packet` | TRACE | `sid`, `dir` (`in`, `out`), `pkt_type`, `frame`, `bytes`, `payload` |
| engineio | `engineio: ping`, `engineio: pong` | TRACE | `sid` |
| socketio | `socketio: connection accepted` | DEBUG | `sid`, `remote_addr` |
| socketio | `socketio: namespace connect` | DEBUG | `sid`, `nsp`, `err` |
| socketio | `socketio: namespace missing` | WARN | `sid`, `nsp` |
| socketio | `socketio: disconnect` | DEBUG | `sid`, `nsp`, `reason` |
| socketio | `socketio: event` | TRACE | `sid`, `nsp`, `event`, `ack_id`, `handler_found`, `duration`, `err` |
| socketio | `socketio: ack` | TRACE | `sid`, `nsp`, `ack_id`, `dir` |
| socketio | `socketio: emit` | TRACE | `sid`, `nsp`, `event`, `ack_id` |
| socketio | `socketio: broadcast` | TRACE | `nsp`, `room`, `event`, `recipients` |
| socketio | `socketio: handler error` | WARN | `sid`, `nsp`, `event`, `err` |
| socketio | `socketio: unhandled error` (no `OnError`) | WARN | `sid`, `nsp`, `err` |
| redis | `redis: publish failed`, `redis: bad message`, `redis: subscriber stopped` | WARN | `nsp`, `channel`, `err` |

DoD: `make lint test-race` green on ubuntu/macos/windows for `stable` and `oldstable`;
an additional Ubuntu job builds/tests the root on Go 1.22 with automatic toolchain
upgrades disabled. From v2 this job covers every shipped runtime module;
`govulncheck` clean; two-instance Redis test under `-race` passes; every (case, side)
pair of the 1.B and 1I test lists is named by a passing test (see 1.B *Gate record*); `engineio/session` coverage ≥ 70%, root
package ≥ 60%; `CHANGELOG.md` lists every fix with the issue or line it addresses.
Logging gate: `TestServerLoggerOption`, `TestLogLevelFromEnv`, `TestLogLevelInvalidEnv`,
`TestWrapOverridesHandlerLevel`, `TestTraceDisabledNoAlloc` and
`TestSessionCloseReason` pass; `TestNoBadKeyAttrs` runs the root scenario from 1.S
at `trace` through a handler that fails on any `!BADKEY` attribute or non-constant
message, and asserts the same `sid` on session open, namespace connect, event and
disconnect; the godoc of package `logger` documents the variable, the levels and the
keys. Links: every badge in `README.md` shows the fork's status. Both greps below print
nothing:

```sh
grep -rnE '\b(log|fmt)\.Print' --include='*.go' . | grep -v '_examples/\|_test.go'
rg -n 'https?://godoc[.]org' -g '*.md' .
```

Acceptance: `_examples/default-http` works unchanged against `socket.io-client` 2.x;
`go get` of the fork at `v1.5.0` builds a consumer that previously used upstream after
updating its imports to the fork's v1 module, without an upstream-path `replace`
directive. Owner runs `SOCKETIO_LOG_LEVEL=debug go run .` in
`_examples/default-http`, opens the browser page, sends one event and closes the tab:
the log shows session open, namespace connect and a disconnect with one `sid`.
A client-sent CLOSE produces `reason="transport close"`; abrupt tab termination may
produce a transport error or ping timeout. With `trace`, the event, ping/pong and
payload lines appear; unset, the application's handler controls the level;
`SOCKETIO_LOG_LEVEL=bogus`
prints one warning and behaves as unset. Every badge and link in `README.md` resolves
on GitHub.

## Stage 1b. Package layout (prerequisite to stage 2)

The first commits on `master` after `v1` is branched from `v1.5.0`; no tag. Structural
refactoring only: moves, explicit file merges, import rewrites and the minimal
interface adaptations listed below; no behaviour change or new features.

Keep the cyclic v1 root core together until the atomic transition in 2.0.

Target tree (root module unless noted):

| Path | Package | Holds |
| --- | --- | --- |
| `.` | `socketio` | public API; v2 adds `Socket`, `Event[T]`, `AckEvent[T, R]`, `Options`, `Adapter` and in-memory adapter; legacy root runtime removed in 2.0 |
| `adapter/` | `adapter` | temporary v1 `Broadcast`, `EachFunc`, `Conn` and `NewMemory`; removed in 2.0; `adapter/codec` added in 2.2 |
| `adapter/codec/` | `codec` | 2.2, shared msgpack encoding |
| `adapter/redis/` | `redis` | temporary v1 Redis broadcast; removed in 2.0 and replaced by the v2 module `adapters/redis` in 4b |
| `adaptertest/` | `adaptertest` | 4b |
| `client/` | `client` | Socket.IO client rewritten in 2.3; v1 root client removed in 2.0 |
| `contrib/otel/` | own module | 2.4 |
| `engineio/` | `engineio` | server side: `server.go`, `options.go` (from `server_options.go` and `types.go`), `conn.go` (from `connect.go`), `hooks.go` (2.4) |
| `engineio/client/` | `client` | `client.go`, `dialer.go` (`Dialer`, `Opener`); depends on `engineio.Conn` only |
| `engineio/session/` | `session` | `session.go`, `manager.go`, `id_generator.go`; `base.go` removed in favour of `frame.Type` |
| `engineio/frame`, `packet`, `payload`, `transport/...` | unchanged | |
| `parser/`, `logger/` | unchanged | |

1. **`engineio/client`**. Move `engineio/client.go` and `engineio/dialer.go` to
   `engineio/client/`; `engineio.Dialer` becomes `client.Dialer`;
   `engineio/server_test.go` becomes package `engineio_test` importing
   `engineio/client`; the root `client.go` and `engineio/_examples` are updated.
2. **engineio file names and frame type**. `connect.go` → `conn.go`; `types.go`
   merged into `options.go` (renamed from `server_options.go`); `session/base.go`
   removed, and `session.FrameType`, `session.TEXT`, `session.BINARY` are replaced by
   `frame.Type`, `frame.String`, `frame.Binary` in `engineio`, `engineio/client`,
   `parser` and root tests; `session_manager.go` → `manager.go`,
   `session_id_generator.go` → `id_generator.go`; the four `engineio/packet/fake_*.go`
   test doubles are merged into `engineio/packet/fake.go`.
3. **`adapter` and `adapter/redis`**. `broadcast.go` → `adapter/memory.go`
   (`adapter.Broadcast`, `adapter.EachFunc`, `adapter.NewMemory`);
   `redis_broadcast.go`, `adapter_options.go` and `helpers.go` → `adapter/redis/`;
   the Redis dial in `Server.Adapter` moves to `redis.Ping`. The root keeps
   `type Broadcast = adapter.Broadcast` and
   `type RedisAdapterOptions = redis.Options` as deprecated aliases and keeps
   `Server.Adapter`; all three are removed in 2.0. `Server.ForEach` wraps the callback
   so the public `EachFunc func(Conn)` is unchanged.
4. **Root file names by role**. `connection_handlers.go` →
   `packet_handlers.go`; `namespace_handlers.go` merged into `namespace_handler.go`;
   `namespaces.go` merged into `connection.go`; `namespace_conn.go` → `namespace.go`;
   `handler.go` → `event_handler.go`; tests follow their files. The `CLAUDE.md` layout
   table is updated and stage 2 paths in this file point at the new tree.

DoD: `make lint test-race examples` green; `go vet ./...` clean;
review `git diff -M --stat v1.5.0..HEAD -- '*.go'` against a source-to-target map,
including merged files; rename detection is informative, not an acceptance gate.
Existing behaviour/regression tests pass without weakening assertions;
no root `*.go` file imports `gomodule/redigo`; `engineio/client` imports nothing from
`engineio` except `engineio.Conn`, and `engineio` does not import `engineio/client`
outside tests. Each command below prints nothing:

```sh
grep -l 'gomodule/redigo' *.go
grep -rn 'session\.\(FrameType\|TEXT\|BINARY\)' --include='*.go' .
# prefix rule: no directory has more than two non-test files sharing a <prefix>_
for d in $(git ls-files '*.go' | grep -v '^_examples/' | xargs -n1 dirname | sort -u); do
  find "$d" -maxdepth 1 -name '*.go' ! -name '*_test.go' -exec basename {} \; |
    sed -n 's/^\([a-z]*\)_.*/\1/p' | sort | uniq -c | awk -v d="$d" '$1>2{print d, $2, $1}'
done
```

Acceptance: owner compares the `CLAUDE.md` layout table with `ls -R` of the module and
finds they match; `_examples/default-http` and `_examples/redis-adapter` build and run
with no source change other than import paths; `go doc ./adapter` and
`go doc ./engineio/client` show the moved API.

## Stage 2. Socket.IO protocol v5 over Engine.IO protocol v4 (tag `v2.0.0`)

Protocol deltas are listed in [PROTOCOL.md](PROTOCOL.md#planned-engineio-v4-and-socketio-v5).
Stage 2 lands in the tree defined by stage 1b. New code for the `Socket` model goes to
the root files `server.go`, `namespace.go`, `socket.go`, `packet_handlers.go`,
`event.go`, `options.go` and `errors.go`; the client goes to `client/`.

### 2.0 Generic API and lifecycle contract

The 2.0 owner atomically removes the legacy root server/client/namespace/handler
runtime, its v1-specific tests, temporary `adapter`/`adapter/redis`, compatibility
aliases, `Server.Adapter` and redigo dependency while introducing the v2 skeleton.
Preserve v1 on its branch; carry applicable regression scenarios into v2 fixtures.
Engine.IO/parser packages remain buildable until their replacements in 2B. Legacy
application examples are excluded from v2 build jobs until migrated in 2.5D; Redis
examples return in 4b. This transition must build/test the entire root module before G2.

Implement the v2 generic API skeleton on Go 1.22 before runtime dispatch.
Positive compile fixtures cover server and client registration,
emit, ack, room broadcast, `Args2` and binary data; negative fixtures must reject a
wrong handler argument, ack return type or emitted payload. Keep these fixtures in
CI. Freeze the shared types consumed by parallel work: `Endpoint`, client registration
interface, `Options`, packet/argument codecs, `Adapter`, both hook structs and result
enums. Publish a method-signature inventory and acyclic package graph. No placeholder
`any` handler, unresolved signature or TODO in these interfaces passes G2. Runtime
work is assigned to 2.1–2.4; the skeleton contains no claimed runtime implementation.

`Event[T].Handle` registers `func(context.Context, *Socket, T) error` on a namespace;
`AckEvent[T, R].Handle` registers `func(context.Context, *Socket, T) (R, error)`.
Both return a registration error. Client descriptors offer `HandleClient` with the
shared `Endpoint` interface in place of `*Socket`. `Endpoint` lives in the root,
is implemented by server sockets and the Go client, and supplies packet send/ack
operations without importing `client/` into the root. Public methods keep the type
relationship on the generic descriptor, not on an untyped namespace method.

Lifecycle contract, implemented in 2.1/2.3 and instrumented in 2.4:

- The handshake uses `r.Context()` until acceptance/rejection. An accepted session
  gets its own cancellable context derived from `context.WithoutCancel` of the
  handshake context, preserving values but not the HTTP deadline/cancellation.
  Session close/server shutdown cancel it; namespace sockets derive cancellable
  children. Returning from polling/upgrade `ServeHTTP` must not cancel the session.
- One reader per Engine.IO session handles protocol control and ack completion
  independently of user handlers. A bounded worker queue per namespace socket runs
  user events sequentially in receive order; namespace auth/middleware also run off
  the reader with a bounded connect deadline. A blocked handler cannot stop heartbeat
  or ack processing; `EmitWithAck` inside a handler is supported. No ordering is
  promised between different sockets or concurrent external emitters; binary packet
  headers and attachments are queued/written as one indivisible message group.
- Resolve each pending ack exactly once on reply, caller cancellation, timeout,
  enqueue failure or disconnect. Use the earlier of the caller deadline and
  `AckTimeout`; ignore late/duplicate ack IDs. Allocate monotonically increasing IDs
  without reuse for the entire Engine.IO connection lifetime, including namespace
  reconnects; never exceed JavaScript's safe integer maximum (`2^53-1`). Exhaustion
  rejects new ack requests with `ErrAckIDExhausted` until a fresh transport session.
  Handler panics are not recovered by the library; document this application contract.
- `Shutdown(ctx)` rejects new handshakes/events, cancels socket/handler contexts and
  pending acks, drains already queued outbound messages until the deadline, then
  closes transports and owned adapter workers. It is idempotent; `Close()` aborts
  immediately. A socket or session close requested by the application drains the
  session's queued outbound messages within the session ping timeout, as v1
  `Conn.Close` does since 1.B; closes started inside the library discard, as in 1.B.
  A peer namespace DISCONNECT ends only that socket and keeps the session queue, as in 1.B.
  The DoD below includes a disconnect-drain test; the v1 to v2 mapping is in 2.5.
  Application handlers must honor cancellation: Go cannot forcibly
  terminate them, and shutdown returns on its deadline even if one does not exit.
  Injected broker clients, loggers and OTel providers remain application-owned.

DoD: compile fixtures pass on Go 1.22; runtime stages add tests for request-context
independence, handler-initiated ack, ordering, concurrent close/ack/timeout, bounded
queues, graceful/forced shutdown and an application close that drains within the ping
timeout while a library close discards, all under `-race`. Include timeout of A, new
request B and late ACK A: B must remain pending until its own terminal condition.

### 2.1 Engine.IO v4 and gobwas/ws

Entry: G2. Exit: Engine.IO suite, transport/client tests and idle-connection baseline.

Reuse the prepared work from the baseline's `codex/eio4-payload` revision. The 2.1
owner ports/rebases it after 1b and G2, preserving tests and updating imports to the
v2 layout; do not repeat completed codec work or merge the branch blindly.

| Prepared component | Evidence in branch | Remaining 2.1 integration |
| --- | --- | --- |
| `engineio/payload/internal/eio4` | polling codec, bounded `DecodeReader`, exact-wire `EncodeBatch`, fixtures/fuzz tests and pinned Node oracles | route through package `payload` to respect `internal` visibility; HTTP POST limits/status mapping, client batching, cancellation/deadlines and polling upgrade/pause lifecycle |
| `engineio/transport/websocket/internal/eio4` | complete-message EIO4 codec, binary/text fixtures and parser oracle | connect codec to the production transport and session lifecycle |
| `_experiments/eio4-websocket` (own module) | gobwas framing prototype: masking, fragments, UTF-8, control frames, message limits and pinned `ws` peer | adapt `FrameReader`/`FrameWriter`, use `ws.Dialer`, preserve options/deadlines, implement protocol-error close status handling and integrate EIO handshake/heartbeat/upgrade |

Preparation policy remains explicit: polling reads are byte-bounded before
buffering, then decoded as a complete batch without an additional packet-count cap.
Decoded heap can exceed wire bytes; retain `BenchmarkDecode/dense-records` and
measure amplification. Incremental decoding is deferred, not an integration gate.
Keep canonical base64/UTF-8 validation and fixtures for intentional differences from
Node: exact base64 batching, oversized-first-packet rejection and no partial decode
on invalid batches. Client `maxPayload` limits POSTs, not server responses. Reuse
locked reference versions, recording changes when refreshed; these oracles establish
component behaviour, not full Go-server conformance. The experiment is outside root
`go test ./...`: run its own race tests and Node oracle until it is retired after
production integration. Re-run all codec, reader, batching and framing checks after
porting; existing branch Go race tests passed during this roadmap review.

- Replace the legacy `engineio/payload`/`pauser` transport integration using these
  codecs; complete the session lifecycle work rather than rewriting codecs again.
- `engineio/session`: server ping ticker and `pingTimeout` to await each pong;
  clients use `pingInterval+pingTimeout` to detect a missing server ping. Add
  `maxPayload` and noop on upgrade. `engineio/server.go`: `EIO` check, JSON errors.
- `engineio/transport/websocket` rewritten on `gobwas/ws`: `ws.UpgradeHTTP` hijacks the
  connection (HTTP/1.1 only); frames read with `wsutil.Reader` and written with
  `wsutil.Writer` so the `FrameReader`/`FrameWriter` contract is preserved; control
  frames handled by `wsutil.ControlFrameHandler`; one write mutex per connection;
  `CheckOrigin`, `ReadBufferSize`, `WriteBufferSize` options kept. Client side uses
  `ws.Dialer`. `gorilla/websocket` removed from `go.mod`. Both `engineio.Server` and
  `socketio.Server` assert `var _ http.Handler`; a wrapped `ResponseWriter` without
  `http.Hijacker` is answered with HTTP 501 and an `engineio: request rejected` line with
  `reason="no hijacker"`, never a panic.
- CI job runs `socketio/engine.io-protocol/test-suite` (Node) against the Go server.
- `BenchmarkIdleConnections` (10k websocket connections, RSS and goroutines) recorded
  in `CHANGELOG.md` before and after the swap.
- Bound decoded message size across polling and fragmented websocket frames, not
  just individual frame size. Initial configurable defaults: message limit 1 MiB,
  handshake and upgrade timeout 10 s each, write timeout 10 s. Preserve protocol
  control processing during handler load. Fuzz malformed payloads, frame boundaries
  and upgrade/close sequences; keep reproducing inputs as regression fixtures.

### 2.2 Adapter interface and memory implementation

Entry: G2. Exit: memory-adapter conformance and dependency-graph checks.

```go
type Adapter interface {
    AddAll(sid SocketID, rooms []Room)
    Del(sid SocketID, room Room)
    DelAll(sid SocketID)
    Broadcast(ctx context.Context, pkt parser.Packet, opts BroadcastOptions) (BroadcastResult, error)
    Sockets(ctx context.Context, rooms []Room) ([]SocketID, error)
    SocketRooms(sid SocketID) []Room
    FetchSockets(ctx context.Context, opts BroadcastOptions) ([]RemoteSocket, error)
    ServerSideEmit(ctx context.Context, event string, args ...any) error
    Close() error
}
type BroadcastOptions struct{ Rooms, Except []Room; Flags BroadcastFlags }
type BroadcastResult struct{ LocalRecipients int; Published bool }
type AdapterFactory func(nsp *Namespace) (Adapter, error)
```

`Adapter`, related types and the in-memory implementation live in root `socketio`;
`adapter/codec` depends on parser/wire types, never on root `socketio`. External
adapters import the root; the root never imports them. This avoids a cycle through
`AdapterFactory(*Namespace)`. The memory adapter is the v2 default; legacy removal belongs to 2.0. The v2.0
release and its example build job require only the memory adapter.

`Broadcast` success means local recipients were queued and, for a non-local cluster
broadcast, publication was accepted by the broker client. It does not mean remote
delivery or client ack. `LocalRecipients` counts successful local enqueues after
room union/deduplication and exclusions; `Published` records broker acceptance, not
remote recipient count. On partial enqueue/publish failure return the partial result
and an error; do not retry implicitly and risk duplicate delivery. `Local` never
publishes. Memory and broker adapters share these rules. Room operations are local;
`Sockets` is cluster-wide and `SocketRooms` is local. Empty room filters select all.
`FetchSockets`/`Sockets` may return deduplicated partial data plus a deadline error;
zero peers must not be confused with a proven empty cluster. `ServerSideEmit` accepts
publication without implying execution on peers. Order is per producer on a live
connection, not global across nodes; disconnect gaps have no replay guarantee.
`Close` releases adapter-owned subscriptions/workers, never injected broker clients.
Conformance tests cover these semantics and concurrent join/leave/broadcast.

### 2.3 Socket.IO v5 and the generic API

Parser work starts at G2; server/client runtime starts after 2B. Exit: typed
Go/Node interoperability, 2.0 lifecycle tests and dispatch benchmark baseline.

- `parser`: CONNECT payload, CONNECT_ERROR object, marker interface instead of
  `Type().Name()=="Buffer"`, `Packet` value type with lazily decoded args.
- New model `Server → Namespace → Socket` replacing `conn`/`namespaceConn`. Explicit
  CONNECT for `/`. Each `Socket` owns a `context.Context` cancelled on disconnect.
- Generics-first public API; reflection-based `OnEvent(string, interface{})` is
  removed:

```go
var Message = socketio.NewEvent[ChatMessage]("message")
var Send    = socketio.NewAckEvent[ChatMessage, Receipt]("send")

Message.Handle(nsp, func(ctx context.Context, s *socketio.Socket, m ChatMessage) error { ... })
Send.Handle(nsp, func(ctx context.Context, s *socketio.Socket, m ChatMessage) (Receipt, error) { ... })

Message.Emit(ctx, s, ChatMessage{...}) // error; s implements Endpoint
r, err := Send.EmitWithAck(ctx, s, ChatMessage{...})
result, err := Message.EmitTo(ctx, nsp.To("room").Except(s.ID()), ChatMessage{...})

nsp.Use(socketio.Auth[Credentials](func(ctx context.Context, s *socketio.Socket, c Credentials) error { ... }))
nsp.OnRaw(func(ctx context.Context, s *socketio.Socket, e socketio.RawEvent) error { ... })
```

- Handler errors are returned, not panicked; `Handle` returns an error on duplicate
  registration; sentinel errors (`ErrNamespaceClosed`, `ErrAckTimeout`,
  `ErrWriteBufferFull`, `ErrSocketClosed`) work with `errors.Is`. `ErrAckTimeout` is
  driven by `socketio.Options.AckTimeout` (default 30 s); every pending ack ends on
  disconnect with `ErrSocketClosed`, so each emit-with-ack has exactly one outcome.
  Caller cancellation preserves `context.Canceled`/`context.DeadlineExceeded` through
  `errors.Is`; the library's own timer returns `ErrAckTimeout`.
- **Wire contract.** Ordinary `T` (including a struct or slice) is one positional
  JSON argument. `Args2[A, B]` expands to exactly two positional arguments; it is not
  one JSON array. Descriptors carry typed encode/decode functions; dispatch itself
  never reflects over handlers. `socketio.Binary` uses standard binary placeholders
  and attachments, including nested fields, slices and maps handled by `parser`.
  Encode/copy payload and binary bytes before `Emit` returns, so later application
  mutation cannot change a queued message. The raw escape hatch exposes positional
  `json.RawMessage` args, attachments and an explicit raw ack responder without
  imposing a typed schema; it supports standard Node/admin event contracts.
- **Typed ack errors.** `AckEvent` uses a documented application-level error-first
  convention: success `[null, ...resultArgs]`, failure `[{"code": "...", "message":
  "..."}]`. This is ordinary Socket.IO ACK data, not a new protocol packet. Node
  callbacks use `(err, result)` (or multiple result arguments for `Args2`). Raw ack
  handlers can interoperate with other application conventions. Known public errors
  have explicit codes; unexpected handler errors expose `internal_error` with a
  generic message and log the original error. Unknown events without a raw handler
  and typed decode failures produce `unknown_event`/`invalid_payload` when an ack
  was requested, otherwise only the error hook/log; malformed protocol envelopes
  close with `parse error`. For valid known events, server and client use this table:

  | Descriptor | Incoming ACK ID | Handler and response |
  | --- | --- | --- |
  | `Event[T]` | absent | run handler; errors go to hook/log only |
  | `Event[T]` | present | run handler; reply `[null]` on success or the typed error envelope |
  | `AckEvent[T, R]` | absent | run handler, discard result; errors go to hook/log only |
  | `AckEvent[T, R]` | present | run handler; reply using the typed ack convention above |

  Tests cover all four combinations with success/error, both Go/Node directions and
  binary ACKs; responses are queued at most once and only when an ACK ID is present.
- **Resource limits.** `socketio.Options` implements separate count and byte budgets:
  outbound queue 64 complete message groups / 8 MiB, handler queue 64 events / 8 MiB,
  128 pending acks per socket, at most 64 binary attachments and 1 MiB reconstructed
  event size, with 10 s attachment assembly timeout. All are configurable; queued
  attachments count toward byte budgets. Per Engine.IO session, allow at most four
  concurrent namespace CONNECT/auth tasks, no waiting queue and no concurrent CONNECT
  for the same namespace. Reject excess/duplicate attempts with CONNECT_ERROR without
  starting middleware. Each task has a 10 s deadline; retain its slot until middleware
  actually returns, even after cancellation, so non-cooperative handlers cannot cause
  unbounded task creation. Test ping/ACK progress during auth saturation.
  Pending-ack saturation rejects the new
  emit with `ErrTooManyPendingAcks`; queue overflow closes the affected session (outbound)
  or namespace socket (handler queue), completes pending operations and reports the
  reason once. Outbound limits apply across all namespaces sharing a transport.
  Add fuzz/regression tests for invalid placeholders, incomplete binary messages,
  argument arity and limits; verify memory remains bounded with a stalled peer.
  Returned errors distinguish oversized messages, too many attachments and queue
  saturation. Document every limit's option name, unit, default and zero-value
  semantics in the 2.0 API fixture; zero selects the bounded default, not unlimited.
- `BenchmarkEventDispatch` (root) is added with the new model, so stage 2.4 has a real
  baseline.
- Rewrite `Client` on the same generic API with websocket over `gobwas/ws`.
- Example migration is owned by 2.5D after runtime and observability gates pass.

### 2.4 Observability

Depends on 2.1 (server ping ticker, close reasons), 2.2 (`Adapter`) and 2.3 (`Socket`
context, typed events, `AckTimeout`). Tracing and metrics go through two hook structs in
the root module with no external dependency; the OpenTelemetry bridge is the separate
module `contrib/otel`. The stage-1 boundary records are produced by
`LoggingHooks`. Every hook must have an exercised fire point and a corresponding log
record; the coverage test verifies both.

```go
package engineio

type SessionInfo struct{ SID, Transport, RemoteAddr string }
type PacketInfo struct{ Type packet.Type; Frame frame.Type; Bytes int; Preview []byte }
type HandshakeResult struct{ Result string; Err error; Duration time.Duration }
type CloseReason string // "transport close", "transport error", "ping timeout", "forced close", "server shutting down", "parse error"

type Hooks struct {
    HandshakeStart  func(ctx context.Context, r *http.Request) context.Context
    HandshakeEnd    func(ctx context.Context, s SessionInfo, r HandshakeResult)
    RequestRejected func(ctx context.Context, r *http.Request, reason string, err error)
    SessionOpen     func(ctx context.Context, s SessionInfo) context.Context // becomes Session.Context()
    SessionClose    func(ctx context.Context, s SessionInfo, reason CloseReason, err error, d time.Duration)
    UpgradeStart    func(ctx context.Context, s SessionInfo, from, to string) context.Context
    UpgradeEnd      func(ctx context.Context, s SessionInfo, from, to string, err error)
    PacketRead      func(ctx context.Context, s SessionInfo, p PacketInfo)
    PacketWrite     func(ctx context.Context, s SessionInfo, p PacketInfo)
    PingSent        func(ctx context.Context, s SessionInfo)
    PongReceived    func(ctx context.Context, s SessionInfo, rtt time.Duration)
}

func ChainHooks(hs ...*Hooks) *Hooks // Start funcs thread ctx left to right; nil fields are skipped
func LoggingHooks(l *slog.Logger) *Hooks
```

```go
package socketio

type SocketInfo struct{ SID, SocketID, Namespace string }
type EventInfo struct{ Socket SocketInfo; Event string; AckID uint64; NeedAck, HandlerFound bool }
type EventResult struct{ Result string; Err error; AckQueued bool; Duration time.Duration }
type EmitInfo struct{ Socket SocketInfo; Event string; AckID uint64 }
type BroadcastInfo struct{ Namespace, Event string; Rooms, Except []Room; Local bool }
type AdapterMessage struct{ Namespace, Kind string; Bytes int } // Kind: broadcast, request, response

type Hooks struct {
    ConnectStart    func(ctx context.Context, s SocketInfo) context.Context
    ConnectEnd      func(ctx context.Context, s SocketInfo, err error, d time.Duration)
    Disconnect      func(ctx context.Context, s SocketInfo, reason string, d time.Duration)
    EventStart      func(ctx context.Context, e EventInfo) context.Context // result is the handler ctx
    EventEnd        func(ctx context.Context, e EventInfo, r EventResult)
    Emit            func(ctx context.Context, e EmitInfo)
    MessageQueued   func(ctx context.Context, e EmitInfo)
    EmitStart       func(ctx context.Context, e EmitInfo) context.Context
    EmitEnd         func(ctx context.Context, e EmitInfo, err error, rtt time.Duration)
    BroadcastStart  func(ctx context.Context, b BroadcastInfo) context.Context
    BroadcastEnd    func(ctx context.Context, b BroadcastInfo, result BroadcastResult, err error)
    WriteBufferFull func(ctx context.Context, s SocketInfo)
    AdapterPublishStart func(ctx context.Context, m AdapterMessage) context.Context
    AdapterPublishEnd   func(ctx context.Context, m AdapterMessage, err error, d time.Duration)
    AdapterReceiveStart func(ctx context.Context, m AdapterMessage) context.Context
    AdapterReceiveEnd   func(ctx context.Context, m AdapterMessage, err error, d time.Duration)
}

func ChainHooks(hs ...*Hooks) *Hooks
func LoggingHooks(l *slog.Logger) *Hooks
func (n *Namespace) Hooks() *Hooks // nil-safe wrapper methods for adapters in other modules
```

Hook lifecycle contract (result/reason values are documented closed enums):

| Operation | Start | Exactly one terminal notification | Data / failure handling |
| --- | --- | --- | --- |
| New-session handshake | HandshakeStart before checker/accept | HandshakeEnd on success or every failure path | result, duration, error; SID may be empty on rejection |
| Invalid request for an existing session | none | RequestRejected | log diagnostic only; never counted as a new handshake |
| Transport upgrade | UpgradeStart | UpgradeEnd on success, timeout, rejection or close | original/target transport; change current transport only on success |
| Namespace connect | ConnectStart | ConnectEnd | count socket only after CONNECT queued successfully |
| Inbound event | EventStart, including unknown/decode-error cases | EventEnd after handler/ack enqueue or rejection | ok, error, no_handler, decode_error, closed; AckQueued is not wire delivery |
| Emit with ack | EmitStart before enqueue | EmitEnd on reply, timeout, cancellation, enqueue failure or disconnect | one pending increment/decrement; result includes canceled and error |
| Broadcast | BroadcastStart | BroadcastEnd | partial local enqueue count and broker acceptance; never infer remote delivery |
| Adapter publish / receive | corresponding Start | corresponding End on success/error | byte size, duration; receive spans encompass decoding and local dispatch |

`RequestRejected` also logs failed initial requests but does not increment handshake
metrics: `HandshakeEnd` is their sole counter/duration source. `SessionOpen` fires
only after a successful handshake, and each open has one close. Hooks receive
borrowed metadata only for the duration of the call; observers retaining it must
copy. `PacketInfo.Preview` is capped at 256 bytes, excludes auth-bearing CONNECT
payloads and is empty by default. Add `engineio.Options.PayloadPreviewBytes` as an
explicit opt-in; capture only for an enabled consumer (TRACE for `LoggingHooks`),
apply redaction before delivery and never retain whole frame buffers for a preview.
The owning Socket.IO layer supplies protocol-aware redaction; Engine.IO does not
import the Socket.IO parser. Standalone Engine.IO applications supply their own
payload redactor. Callback retention rules and disabled-capture behaviour are tested.
Keep preview collection disabled when no redactor is configured; enabling TRACE
alone never enables v2 payload capture. The observability example documents this
intentional difference from the temporary v1 tracing behaviour.

Start hooks thread context left to right; terminal hooks run right to left so
cleanup unwinds. Install `ChainHooks(opts.Hooks, LoggingHooks(log))`: tracing creates
the span before its start log, and terminal logging runs before span completion.
Session context is detached from HTTP cancellation per 2.0; the OTel bridge retains
the handshake span context for links while clearing the active parent for new event
spans. Test hook ordering and trace IDs on both start and terminal log records.
Serialize session open/upgrade/close notifications per session so gauge transfers
cannot race with close. Event contexts are retained with queued jobs and receive
their terminal hook even if shutdown discards the job before the handler runs.

Implementation split:

- **2.4E:** fire Engine.IO hooks from handshake, session, upgrade, packet and
  heartbeat paths according to the lifecycle table. Count packet bytes at the
  reader/writer boundary, including error paths.
- **2.4S:** fire Socket.IO hooks from namespace/event/ack/queue/broadcast paths;
  adapters call paired hooks through `Namespace.Hooks()`. Pass the context returned
  by `EventStart` to the handler.
- **2D:** propagate instance loggers before E/S instrumentation is merged.

Instance logger contract (wave 2D): resolve Socket.IO Logger > Engine Logger >
`logger.Log`; standalone engine servers and clients/dialers accept their own
logger. Nil fallback follows the current `slog.Default()`. Propagate the selected
logger through sessions, namespaces, codecs, transports and pre-session dial
failures. Stateless codecs may return errors for their owner to log once.
`Namespace.Logger()` supplies adapter/contrib diagnostics. Copy inherited options;
do not mutate caller options, shared transports or the application's default logger.

Every instance logger passes through `logger.Wrap`, preserving handler, attributes
and groups. `SOCKETIO_LOG_LEVEL` is read once at init; a valid value or runtime
`logger.Level.Set` overrides levels process-wide. `LevelUnset` delegates Enabled
to each handler. Invalid environment values warn once and act as unset. TRACE is
`slog.LevelDebug-4`, rendered by opt-in `logger.ReplaceAttr`. No logger backend
dependency is added; injected broker-client internal logs remain application-owned.

**Logging and overhead.** The inline stage 1 boundary records are removed where a
hook now exists, and `LoggingHooks` emits the same messages and keys, plus `rtt` on
pong, `bytes` on packet, `rooms`, `except` and `local` on broadcast, and
`socketio: adapter publish` / `socketio: adapter receive` start/end lines at `TRACE`.
Broadcast `recipients` means successful local enqueues; also log `published`.
`TestHooksCoverEveryHookPoint` (root) reflects over both structs, runs one scenario
(handshake, upgrade, ping, connect with auth, event with ack, emit with ack,
broadcast, buffer overflow, disconnect) with a recording hook chained before
`LoggingHooks` at `trace`, including rejection, decode failure and adapter error
sub-scenarios, and fails if any field was not called or has no log record
with its message; adding a hook field without a log line therefore fails the build.
`BenchmarkEventDispatch` gets the sub-benchmarks `no-hooks`,
`logging-hooks-at-error` and `recording-hooks`.

**2.4O: `contrib/otel`.** Own `go.mod` (`.../contrib/otel/v2`), CI job and
`README.md`. `otelsocketio.NewHooks(opts ...Option) (*engineio.Hooks,
*socketio.Hooks)` with `WithTracerProvider`, `WithMeterProvider`,
`WithPropagators`, `WithoutTraces`, `WithoutMetrics`, `WithEventAllowList`;
`otelsocketio.NewSlogHandler(next slog.Handler) slog.Handler` adds `trace_id` and
`span_id` from the record context while preserving the supplied handler's sink,
attributes, groups and `Enabled` behaviour; it composes with `logger.Wrap` and
never changes the global logger. Trace context is extracted from the
`*http.Request` in `HandshakeStart`. The ctx returned from `SessionOpen` carries a
span context only (not a recording span), so later spans link to the handshake
instead of parenting under an ended span. Tests use `sdktrace/tracetest` and the
`sdkmetric` `ManualReader`; `BenchmarkEventDispatchOtel/noop` measures overhead.

Spans:

| Span | Kind | Parent / links | Attributes |
| --- | --- | --- | --- |
| `engineio.handshake` | Server | parent: incoming `traceparent` or the middleware span in `r.Context()` | `sid`, `transport`, `remote_addr`, `http.request.method`, `result` |
| `engineio.upgrade` | Internal | link: handshake | `sid`, `from`, `to`, `error` |
| `socketio.connect {nsp}` | Server | link: handshake | `sid`, `socket_id`, `nsp`, `error` |
| `socketio.event {nsp} {event}` | Consumer | link: handshake; ends at `EventEnd` | `sid`, `socket_id`, `nsp`, `event`, `ack_id`, `handler_found`, `error` |
| `socketio.emit {nsp} {event}` | Producer | child of caller ctx; emit-with-ack only; ends at `EmitEnd` | `sid`, `socket_id`, `nsp`, `event`, `ack_id`, `result` |
| `socketio.broadcast {nsp} {event}` | Producer | child of caller ctx | `nsp`, `event`, `rooms.count`, `except.count`, `local`, `recipients` (local), `published` |
| `socketio.adapter.publish {nsp}` | Producer | child of broadcast | `nsp`, `kind`, `bytes`, `error` |
| `socketio.adapter.receive {nsp}` | Consumer | root: the Redis message format is fixed for Node compatibility and carries no trace context | `nsp`, `kind`, `bytes`, `error` |

Instruments (Prometheus names replace dots with `_` and add unit suffixes):

| Instrument | Kind, unit | Attributes | Hook |
| --- | --- | --- | --- |
| `engineio.handshakes` | Counter | `transport`, `result` (`ok`, `bad_transport`, `checker`, `accept`, `no_hijacker`, `init`, `timeout`, `closed`) | HandshakeEnd only |
| `engineio.handshake.duration` | Histogram, s | `transport`, `result` | HandshakeEnd |
| `engineio.sessions` | UpDownCounter | `transport` | SessionOpen, successful UpgradeEnd (old -1, new +1), SessionClose |
| `engineio.session.duration` | Histogram, s | `transport`, `reason` | SessionClose |
| `engineio.upgrades` | Counter | `from`, `to`, `result` | UpgradeEnd |
| `engineio.packets` | Counter | `direction`, `type`, `transport` | PacketRead, PacketWrite |
| `engineio.packet.size` | Histogram, By | `direction`, `transport` | PacketRead, PacketWrite |
| `engineio.ping.rtt` | Histogram, s | `transport` | PongReceived |
| `socketio.sockets` | UpDownCounter | `nsp` | ConnectEnd (ok), Disconnect |
| `socketio.connects` | Counter | `nsp`, `result` (`ok`, `error`, `unknown_namespace`) | ConnectEnd |
| `socketio.socket.duration` | Histogram, s | `nsp`, `reason` | Disconnect |
| `socketio.events.received` | Counter | `nsp`, `event`, `result` (`ok`, `error`, `no_handler`, `decode_error`, `closed`) | EventEnd |
| `socketio.event.duration` | Histogram, s | `nsp`, `event` | EventEnd |
| `socketio.events.sent` | Counter | `nsp`, `event` | MessageQueued once per successful local enqueue, including adapter receive |
| `socketio.acks.pending` | UpDownCounter | `nsp` | EmitStart, EmitEnd |
| `socketio.ack.rtt` | Histogram, s | `nsp`, `event`, `result` (`ok`, `timeout`, `closed`, `canceled`, `error`) | EmitEnd |
| `socketio.broadcasts` | Counter | `nsp`, `local` | BroadcastEnd |
| `socketio.broadcast.recipients` | Histogram, {socket} | `nsp` | BroadcastEnd, LocalRecipients only |
| `socketio.write_buffer.overflows` | Counter | `nsp` | WriteBufferFull |
| `socketio.adapter.messages` | Counter | `nsp`, `direction`, `kind`, `result` | AdapterPublishEnd, AdapterReceiveEnd |
| `socketio.adapter.message.size` | Histogram, By | `nsp`, `direction` | AdapterPublishEnd, AdapterReceiveEnd |

`MessageQueued` has a TRACE log and is the sole source of sent-event counts;
broadcasts do not add another count on top of per-recipient enqueues. `Emit`
describes a fire-and-forget attempt, not delivery. Normalize raw/unregistered
event names to `_unknown` and dynamic namespaces to their registered pattern
before creating metric attributes; raw names may remain in logs/spans. Session
duration uses final transport; it does not represent duration per transport.

**Documentation.** `docs/OBSERVABILITY.md` owns `SOCKETIO_LOG_LEVEL`, the levels, the
log keys, the hook contract (goroutine, non-blocking, no `Emit`, no panic recovery),
the cardinality rule and the span and instrument catalogue. `CLAUDE.md` gets the
ownership row and the `contrib/otel/` layout row; `README.md` gets one line linking
to it. `TestObservabilityDocLists` (root) reflects over both `Hooks` structs and
fails if a field name is missing from the doc.

DoD for 2.4: `TestHooksCoverEveryHookPoint`, `TestChainHooksOrder`, `TestNilHooks`,
`TestEmitWithAckEndsOnDisconnect`, `TestEmitWithAckTimeout`,
`TestSessionContextPropagates` and `TestObservabilityDocLists` pass under `-race`;
`benchstat` of `BenchmarkEventDispatch/no-hooks` between the last 2.3 commit and the
merge of the 2.4 logging implementation shows 0 added allocs/op and at most 2% more time. Use the same
dedicated host, pinned Go/build settings and GOMAXPROCS, at least ten alternating
baseline/candidate runs and recorded benchstat confidence intervals. An inconclusive
comparison is rerun on that host, not accepted/rejected from a noisy shared CI run;
`BenchmarkEventDispatchOtel/noop` is at most 4 allocs/op and 1 µs/op above `no-hooks`;
both results are recorded in `CHANGELOG.md`; `go mod graph` of the root module has no
`go.opentelemetry.io`. In `contrib/otel`: `TestSpans` asserts one span per row of the
span table with the listed attributes and terminal conditions from the lifecycle
table, including events without ACK, rejection and failed ACK enqueue; `TestInstruments` asserts one series per
instrument; `TestMetricAttributesBounded` fails on `sid`, `socket_id`, `ack_id`,
`remote_addr` or an unregistered event in any metric attribute;
`TestHandshakeParentsUnderMiddlewareSpan` puts a recording span in the request context
and asserts the parent relation; `TestSlogHandlerAddsTraceID` passes.
`TestNoBadKeyAttrs` from stage 1 still passes at `trace`.
Add `TestHandshakeEndsExactlyOnce`, `TestSessionGaugeAcrossUpgrade`,
`TestMessageQueuedCountsOnce`, `TestAdapterSpanLifecycle` and
`TestPreviewDisabledNoAlloc`; assert no negative or stranded active-session/pending-ack
series after failures, cancellation and close. Stage 2.4 uses the memory adapter and
a recording adapter fixture for publish/receive failures; no Redis/NATS dependency.

Logger gate: under `-race`, run two servers and two clients with distinct recording
handlers plus a separate global handler. Cover rejected handshake, malformed codec
input, polling, upgrade, dial failure, events and disconnect. Explicit instance
records reach only their handler; nil fallback follows later `slog.SetDefault`.
Check precedence, independent thresholds without override, common threshold with
override, unchanged caller options/transports and preserved handler attrs/groups.
Repeat handler composition through the OTel wrapper and assert trace/span IDs.

Acceptance for 2.4: add `_examples/observability` (own module), a minimal single-server
`/chat` event fixture with memory adapter, OTLP collector and Jaeger compose stack.
Owner runs it with `SOCKETIO_LOG_LEVEL=trace`. A browser message appears as a
`socketio.event /chat message` span linked to that client's `engineio.handshake` span;
the log line for the same event carries the span's `trace_id`; `/metrics` shows
`socketio_events_received_total{nsp="/chat",event="message"}` incrementing and
the sum of `engineio_sessions` across transports equals the number of open tabs.
An explicit client disconnect logs its close reason and decrements the gauge;
abrupt tab termination is separately tested through timeout/transport failure.
Owner also runs two server instances in one process with separate application
handlers and confirms each receives only its instance's diagnostics, including
transport and codec errors, without calling `slog.SetDefault`.

### 2.5 Docs and release

`docs/MIGRATION.md` (including how v1 `Conn.Close` draining maps to v2 socket and
session close), `docs/PROTOCOL.md` update, `docs/OBSERVABILITY.md`,
`contrib/otel/README.md`, module path `.../v2`, tag `v2.0.0` and
`contrib/otel/v2.0.0` from the same commit; branch `v1` created from `v1.5.0`. After the
v2 module path changes: README GoDoc badge and API reference link, `go.mod` and imports of
every `_examples/*`, links in `engineio/README.md`, and the import paths of
`contrib/otel` and `adapters/*`. Task 2.5D migrates all non-Redis examples to
`socket.io-client@4` and the generic API, pinning maintained framework versions
compatible with Go 1.22; Redis examples remain deferred to 4b.

DoD: both official suites (`engine.io-protocol/test-suite`,
`socket.io-protocol/test-suite`) pass in CI against the Go server; a CI Node script with
real `socket.io-client@4` covers connect, namespace with auth, ack both ways, binary,
disconnect, reconnect after server restart; parser and payload unit tests cover every
example in the two specs; `go mod graph` shows no `gorilla/websocket`, and a lint rule
forbids direct `reflect` imports in production code outside `parser` (test reflection
is allowed); all stage-2 examples build and run against
`socket.io-client@4`; `go vet`, lint, `-race` green; idle-connection benchmark numbers
recorded; `docs/PROTOCOL.md` lists every unimplemented item; the stage 2.4 DoD holds at
the tag. Router integration: the `examples` CI job starts `_examples/default-http`,
`gin-gonic`, `go-echo`, `iris` and `gf`, and `TestFrameworkSmoke` in `_examples/smoke`
(own `go.mod`) completes a websocket handshake and one event with ack through each with
the Go client; a test in `engineio` with a `ResponseWriter` that hides `http.Hijacker`
gets HTTP 501. Links: `pkg.go.dev/github.com/sshaplygin/go-socket.io/v2` renders the
tagged version, and the grep below has no active v2 code/module imports of the old
path; historical v1 decisions, migration examples and changelog entries are allowed:

```sh
grep -rn 'googollee' --include='*.md' --include='go.mod' --include='*.go' .
```

Acceptance: a browser page on `socket.io-client@4` from CDN connects to
`_examples/default-http`, joins `/chat`, receives a typed ack, and the server logs a
clean disconnect on an explicit client disconnect. `docs/MIGRATION.md` is enough to port
`_examples/gin-gonic` without reading library code. A handler with a wrong payload type
fails at compile time. Every badge and link in `README.md` resolves to the v2 module.
Pin Node, protocol-suite commits and client versions in CI; test source consumers
against released root/contrib module versions without workspace `replace` directives.
Publish the root tag before dependent module tags and verify each module's minimum
Go version. Redis examples and cluster acceptance are explicitly deferred to 4b.

## Stage 3. Realtime chat example (`_examples/chat/`, own go.mod)

- **Server**: namespace `/chat`; `Auth[Credentials]` middleware reading the nickname
  from the auth payload; rooms; typed events `Message` (ack returns id and timestamp),
  `Typing`, `History` (ring buffer of 50 per room), `Presence` on join/leave; direct
  messages via the socket-id room; binary image attachment; graceful shutdown;
  `/metrics` through `contrib/otel` and `otel/exporters/prometheus`.
- **Clients**: `index.html` with no build step on a pinned `socket.io-client@4` from
  CDN; Go CLI on `client/` and generic event descriptors; `cmd/load` (N Go clients,
  p50/p99 ack latency), with the documented error-first typed ack convention.
- **Deployment**: single server with the memory adapter, OTLP collector and Jaeger
  in `docker-compose.yml`; reuse the stage 2.4 observability configuration. The
  two-node Redis/NATS profile is implemented and accepted in stage 4b.
- **Test**: integration test starting the server and two Go clients, checking history
  and presence, run in the CI examples job.

DoD: `docker compose up` in `_examples/chat` starts in one command; a message sent by
one client reaches another in the same room; `cmd/load` with 500 clients at 10 msg/s
reports p99 ack latency and no write-buffer overflow; integration test green in CI;
`_examples/chat/README.md` documents only how to run it.

Acceptance: owner runs the single-server compose stack, opens two browser tabs,
exchanges messages, sees typing and presence, uploads an image and verifies graceful
shutdown. Define the load rate as per-client (5000 messages/s total), fix message
size, room/fan-out distribution and run duration, and record host/Go settings with
latency, errors, queue peaks and memory; do not use an unspecified workload as a gate.

## Stage 4b. Independent adapters: Redis and NATS

Each adapter has its own `go.mod` and CI job and is tagged independently
(`adapters/redis/v2.0.0`, `adapters/nats/v2.0.0`). The root `go.mod` has no Redis or
NATS dependency. Both depend on the root module as a normal versioned dependency and on
`adapter/codec` for the message format. Release root `v2.2.0`, containing
`adaptertest` and any shared-codec additions, before tagging adapter modules. Verify
root and adapter consumers/tests against published versions without local replacements.

- **`adapters/redis`**: compatibility with the non-sharded
  [`@socket.io/redis-adapter@8.3.0` wire format](https://github.com/socketio/socket.io-redis-adapter/blob/8.3.0/lib/index.ts)
  for stage 2.2 operations; cluster broadcast-with-ack is excluded. Channels:
  `<prefix>#<nsp>#`, `<prefix>#<nsp>#<room>#`, `<prefix>-request#<nsp>#`,
  `<prefix>-response#<nsp>#` and targeted `<prefix>-response#<nsp>#<uid>#`.
  Broadcast messages are msgpack `[uid, packet, opts]` via `vmihailenco/msgpack/v5`
  matching notepack output; supported request/response messages use Node's JSON
  encoding. Freeze fixtures for every supported operation, including
  `publishOnSpecificResponseChannel=true` and false. Inject `redis.UniversalClient`;
  bound request time and reconnect subscriptions with backoff.
- **`adapters/nats`**: subjects `<prefix>.<encoded-nsp>.broadcast` and
  `<prefix>.<encoded-nsp>.room.<encoded-room>`. Encode each arbitrary UTF-8 name as
  `b` plus unpadded base64url of its bytes (empty name becomes `b`); dots, wildcards
  and whitespace never become subject syntax. Validate the configured prefix and
  test empty, Unicode, dotted and wildcard-containing names. Broadcasts use the
  shared msgpack body. `Sockets`/`FetchSockets` use an explicit reply-inbox
  subscription for multiple responses, unique request IDs and a bounded deadline;
  do not use a first-response-only request helper. Snapshot expected peers from
  heartbeat membership, deduplicate replies by server ID and return partial data
  plus an error for missing peers. Document membership staleness and distinguish
  known zero remote peers from unavailable discovery. `ServerSideEmit` follows the
  publication-only contract in 2.2. Inject `*nats.Conn`; reconnect is handled by the
  client. Test with an embedded `nats-server/v2`, no Docker.
- Both adapters report through `Namespace.Hooks()` (paired `AdapterPublishStart/End`
  and `AdapterReceiveStart/End`) and use `Namespace.Logger()` for other library-owned diagnostics;
  add isolation tests proving that two adapters attached to differently configured
  servers do not send logs to each other's handlers or the global fallback.
- **`adaptertest`** package in the root module: conformance suite any adapter runs
  against itself (like `fstest.TestFS`): join/leave, broadcast to room, except, local
  flag, fetch across two adapters, server-side emit, peer loss with timeout, and both
  adapter hooks firing.
- `docs/ADAPTERS.md`; `adapters/<name>/README.md` for backend options; chat example
  supports both backends.
- Restore the migrated Redis examples and add the chat's two-server compose
  profile, selected with `ADAPTER=redis|nats`, with nginx affinity for polling.
  Acceptance routes clients to explicit backends (direct URLs or distinct test
  client addresses); two tabs behind `ip_hash` alone do not prove cross-node traffic.
  History remains an explicitly documented per-node in-memory demo buffer; this
  stage does not introduce durable or globally ordered chat history.
- Adapters close their subscriptions and workers while leaving injected broker
  connections usable. Tests cover peer loss, reconnect gaps, late/duplicate replies,
  partial results, `Local` isolation and repeated close. Redis multi-peer queries
  obey the same partial-result contract, using its protocol's peer-count discovery.

DoD: `go mod graph` of the root module contains no redis or nats module; `adaptertest`
passes for in-memory, Redis and NATS; Redis suite (testcontainers, two servers) and NATS
suite (embedded server, two servers) pass under `-race`; cross-language CI test: one Go
server and one Node `socket.io@4` server with `@socket.io/redis-adapter` share Redis, a
room broadcast from each side reaches a client on the other, and `fetchSockets` from
Node lists the Go socket. Msgpack fixtures captured from notepack cross-decode in
both directions with equal semantics; require exact bytes only for canonical
fixtures where map ordering and numeric representation are fixed.

Acceptance: the chat cluster runs with one Go and one Node instance behind the same
nginx on Redis, and with two Go instances on NATS, and both tabs see each other's
messages in each configuration.
The Go/Go profile additionally verifies that terminating node A leaves node B's
existing sessions alive and observable. Connection recovery to the failed node,
replay of missed messages and shared history remain outside this acceptance.

## Stage 5. Socket.IO Admin UI (required final product stage)

Entry: M5. Output: module `github.com/sshaplygin/go-socket.io/contrib/admin/v2`
with its own CI/release and direct connection from the unchanged official UI.
Support hosted and self-hosted static UI. Pin an upstream UI release and matching
instrumentation commit in 5A; record them in the module README. Reference contracts:
[typed events](https://github.com/socketio/socket.io-admin-ui/blob/develop/lib/typed-events.ts),
[instrumentation](https://github.com/socketio/socket.io-admin-ui/blob/develop/lib/index.ts).
Node is a compatibility-test dependency, not a Go server runtime dependency.

1. **Observation API and lifecycle.** Add concurrency-safe snapshots of namespaces,
   sockets, rooms and serializable handshake/data, plus notifications for namespace
   creation, room join/leave and explicit socket-data updates. Reuse stage 2.4 hooks
   for connection, transport, packet and event metadata. Add opt-in observation of
   incoming and outgoing event arguments, including broadcasts, for the detailed UI;
   payload capture is disabled when unused. Define an explicit data-update API rather
   than trying to observe arbitrary mutations of application-owned Go values. Keep
   additions compatible with the released v2 API. Hook additions also update
   `LoggingHooks`, hook coverage tests and `docs/OBSERVABILITY.md`.
   Admin instrumentation inherits the owning server's logger through its namespace;
   authentication, queue overflow and shutdown diagnostics obey the stage 2.4
   instance isolation contract, verified with distinct handlers on two servers.
   Define snapshot/update synchronization before implementing UI handlers. For each
   Go node, atomically register an observer and capture a state snapshot with a
   monotonic local watermark under the same state synchronization boundary. Buffer
   subsequent updates, send one combined `all_sockets`, then replay only updates
   after each node's watermark through the same ordered admin queue. Internal
   sequence/server IDs stay internal; the official UI wire format is unchanged.
   No global atomic cluster snapshot is promised. Extend the optional admin adapter
   capability to carry node snapshots/watermarks; ordinary `FetchSockets` alone
   cannot supply this synchronization boundary. Overflow or peer loss during initial
   sync aborts it and forces a fresh snapshot rather than presenting partial state
   as complete. Node instrumentation has no such watermark contract: mixed Go/Node
   observation is best-effort during concurrent mutations, with reconciliation
   after quiescence via a fresh snapshot/reconnect; validate convergence against the
   pinned UI and Node package. Node failure detection marks its sockets stale and
   removes them through supported UI events; do not rely only on missing stats.
2. **Admin namespace and authentication.** Provide `admin.Instrument` with a
   configurable namespace (default `/admin`), unique `serverId`, authentication,
   session store, mode and read-only option. Accept `username`/`password` or
   `sessionId` in the namespace handshake auth; emit `session` for session reuse.
   Require configured authentication or explicit opt-out. Default to read-only;
   enforce it on the server as well as in advertised features. Support a shared
   session store for clustered deployments. Document the UI origin, CORS credentials
   and WebSocket origin settings needed for hosted and self-hosted UI connections.
3. **Statistics and detailed observation.** Emit `config` with only implemented
   `supportedFeatures`, and `server_stats` every two seconds with server identity,
   uptime, connection counts, namespace counts and `aggregatedEvents`. Production
   mode sends aggregate statistics; development mode additionally sends `all_sockets`
   and live `socket_connected`, `socket_updated`, `socket_disconnected`, `room_joined`,
   `room_left`, `event_received` and `event_sent` updates. Match upstream argument
   order, timestamps, binary handling and serialized socket fields; distinguish an
   Engine.IO session from its namespace sockets. Exclude admin traffic from detailed
   event observation to prevent recursion. Never serialize handshake auth; provide
   redaction for headers, socket data and event arguments.
4. **Administrative commands and cluster support.** Implement `emit`, `join`,
   `leave` and `_disconnect`, including namespace/room filters and the distinction
   between namespace disconnect and transport close. Add an optional adapter
   capability interface for `SocketsJoin`, `SocketsLeave` and `DisconnectSockets`
   rather than breaking the stage 2.2 `Adapter` interface. Implement it for memory,
   Redis and NATS; extend `adaptertest` with remote operations and peer-loss deadlines.
   Freeze result/error semantics in 5A: success means local application plus broker
   acceptance for remote commands, not confirmed execution by peers. Return partial
   local results and publication errors without implicit retries. Match Node's
   unacknowledged bulk join/leave/disconnect encoding; deadlines bound publication
   and snapshot queries, not remote execution. Tests separately observe remote state.
   Use the synchronized snapshot
   capability for Go nodes and `FetchSockets` for the documented Node fallback;
   deliver each server's statistics and live updates to admin clients
   across the cluster, and avoid duplicate observations. Advertise bulk command
   features only when the active adapter supports them.
5. **Bounded delivery and shutdown.** Hooks only record or enqueue observations;
   they never emit or block on the UI. Separate workers send updates through queues
   bounded by both record count and bytes per admin consumer, including initial-sync
   buffering. Define and test overflow handling: count dropped updates and
   terminate the slow admin connection so reconnect obtains a fresh snapshot rather
   than silently retaining stale state. Cap payload capture size, stop workers and
   tickers on close, and release observers. Benchmark disabled instrumentation,
   production mode and detailed mode against the same server workload; disabled
   payload observation adds no allocations to event dispatch.
6. **Documentation, example and release.** `docs/ADMIN_UI.md` owns the event contract,
   supported features, modes, lifecycle, buffering and cluster behaviour;
   `contrib/admin/README.md` owns installation and configuration. Update the
   `CLAUDE.md` ownership/layout tables and add a README link. Extend the chat compose
   example with a pinned static Admin UI service and authenticated instrumentation
   on each server. Release additive core changes as `v2.3.0`, the module as
   `contrib/admin/v2.0.0`, and compatible adapter updates as minor releases.

DoD: event fixtures from the pinned upstream instrumentation match the Go output,
including positional arguments and date encoding. Browser tests against the actual
pinned UI cover authentication, reconnect/session reuse, statistics, socket and room
lists, transport upgrades, binary event display, live updates and administrative
commands. Tests prove that read-only mode rejects commands, payload redaction holds,
admin events do not recurse, slow admin consumers do not stall application sockets,
and shutdown leaks no library-owned goroutines. During initial sync and reconnect,
continuously connect/disconnect sockets and change rooms on two nodes; assert that
the UI converges without resurrected sockets, missed removals or duplicate entries.
Exercise snapshot queue overflow and actual UI reconnect, plus the documented
mixed-Node fallback after quiescence. Race tests and the expanded `adaptertest` pass for
all three adapters. Cluster tests cover two Go nodes with Redis and NATS and a mixed
Go/Node Redis cluster with instrumentation on both nodes: an admin client connected
to one node sees remote sockets and server statistics and can administer remote
sockets. Benchmark results and tested upstream versions are recorded in the release
notes; earlier protocol and observability checks remain green.

Acceptance: owner starts the chat compose stack and opens the unchanged Admin UI.
The UI shows both servers and their live connections; sending messages, joining and
leaving rooms and closing a tab update the corresponding views. With administrative
commands enabled, a remote socket can join/leave a room and be disconnected. Killing
one server is visible in the UI while the surviving server remains observable;
reconnecting the UI refreshes its state. Repeat with Redis and NATS. Passing this
acceptance completes product scope and opens the final benchmark stage.

## Stage 6. Final comparative benchmarks

Entry: M6, including Admin UI and released adapter updates. Earlier microbenchmarks
remain regression gates; this stage compares complete servers after product work.
Owner freezes the matrix in 6A before measurements; runner implementation can then
proceed independently in 6B. Reserved measurement hosts run one candidate at a time.

**Candidates and compatibility.** Compare our final v2, `zishang520/socket.io` and
the official `socket.io` JS/TS server running on Node.js. Pin exact commits/tags,
Go/Node versions, dependency lockfiles and build flags. Also record this fork's
`v1.5.0` as a legacy baseline for shared scenarios, with a compatible EIO3 client;
report it separately from EIO4 comparisons. A feature support matrix must mark
unsupported cases, never score them as zero throughput. Benchmark wrappers implement
the same application logic and wire payload/ACK contract; TS source is built before
timing, with no development server or runtime transpilation.

**Workload matrix.** Freeze exact payload sizes, room topology, connection counts,
offered rates and run lengths in a checked-in manifest. Required scenarios:

| Scenario | Controlled dimensions | Primary measurements |
| --- | --- | --- |
| Idle and connection churn | 1k/10k sessions; websocket-only, polling-only, polling-to-websocket upgrade | RSS per session, CPU, connect latency, failures, cleanup |
| Events and ACKs | one-to-one; text and binary at 64 B, 1 KiB and 64 KiB; fixed-rate load and saturation sweep | delivered msg/s and bytes/s; p50/p95/p99 latency, timeout/error/drop rate |
| Room broadcast | fixed room membership/fan-out; memory and two-node Redis | recipient deliveries/s, end-to-end latency, broker load |
| Overload and recovery | stalled receivers, bounded queues, disconnect/reconnect and server drain | healthy-client latency, memory/queue peaks, rejected work, recovery time |
| Product observability | disabled; comparable aggregate metrics/logging; Admin UI production and detailed modes with one real connected UI | throughput/latency/memory cost relative to each candidate's disabled mode |

For unsupported observability/adapter combinations, report our feature overhead
separately; do not compare it with an uninstrumented competitor. NATS is an own-v2
extension unless a candidate supports equivalent semantics. Retain the prepared
dense-polling microbenchmark alongside server RSS measurements; wire-byte limits
alone do not establish decoded-memory parity.

**Measurement protocol.** Use identical server CPU/memory limits, network/TLS and
compression settings, heartbeat/size/queue policies where configurable, and broker
versions/topology. Record differences that cannot be aligned. Publish separate
single-core and fixed multicore-budget results, including process/worker topology;
do not compare unrestricted Go scheduling against a CPU-capped Node process.
Keep load generators and brokers off server measurement cores; verify generator
headroom. Use the same pinned client generator for EIO4 candidates and validate
traffic/counts before timing. Use an open-loop fixed-rate phase with latency from
scheduled send time, so slow replies do not silently reduce offered load; report
scheduled, sent, acknowledged and delivered totals, including failed/time-out work.
Freeze saturation criteria in 6A: maximum sustainable rate requires p99 <= 100 ms,
errors/timeouts <= 0.1%, and no sustained queue/RSS growth in the measured window.
Report other operating points too; this definition is not a promise that v2 wins.

Use at least 30 s warm-up and 120 s measurement, ten independent runs per published
comparison, randomized candidate order and a fresh process per run. Record host/OS,
CPU affinity, runtime/GC settings, resource counters and confidence intervals.
Keep runtime-specific GC/heap/alloc/goroutine or event-loop diagnostics separate
from cross-runtime metrics; archive raw samples and analysis scripts. Correctness
checks run before and after load; missing deliveries are counted, not discarded.

**Deliverables and gate.** Add an isolated `benchmarks/` harness with independent
runner modules/manifests, one documented command per scenario, and
`docs/BENCHMARKS.md` owning methodology, support matrix and results. Store raw data,
configs, checksums and charts as versioned downloadable artifacts; update the
CLAUDE.md ownership map and link the report from README. CI validates runners and a
short smoke scenario; full results come from reserved hosts. Acceptance requires
complete comparable results, recorded unsupported cases and an independent rerun
of a representative scenario within the published uncertainty, not a preselected
performance ranking. Investigate mismatches and limitations in the report. If this
stage triggers code changes, rerun affected correctness gates and comparisons with
new revision IDs; release them separately. M7 closes the roadmap.

## Milestones

| Milestone | Content | Tag |
| --- | --- | --- |
| M0 | Stage 0 docs baseline | none |
| M1 | Stage 1 | `v1.5.0` |
| M1b | Stage 1b package layout | none (first commits after branch `v1`) |
| M2 | 2.0 generic API/lifecycle contract + 2.1 Engine.IO v4 on gobwas/ws + conformance | branch `v2-dev` |
| M3 | 2.2 + 2.3 + 2.4 + 2.5 | `v2.0.0`, `contrib/otel/v2.0.0` |
| M4 | Stage 3: single-server chat | `v2.1.0` |
| M5 | Stage 4b: adapters and cluster chat acceptance | root `v2.2.0` first, then `adapters/redis/v2.0.0`, `adapters/nats/v2.0.0` |
| M6 | Stage 5: Admin UI observation and cluster administration | `v2.3.0`, `contrib/admin/v2.0.0`; adapter minor releases |
| M7 | Stage 6: final comparative benchmark report and reproducible artifacts | report/artifact revision; no runtime release required |

Re-estimate stage 2 after G2 and adapters after the shared codec/conformance fixtures.
The earlier 15–25 working-day estimate for stage 5 is provisional; measure the
snapshot/Node interoperability prototype before treating it as a delivery commitment.

## Out of scope

EIO=3 in v2; connection state recovery; WebTransport; permessage-deflate; sharded Redis
adapter (Redis 7 sharded pub/sub); cluster broadcast-with-ack; NATS JetStream persistence;
framework-specific integration packages (gin, echo, iris, gf use `http.Handler`); trace
context propagation through the Redis adapter.
