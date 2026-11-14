# V2 roadmap

This plan concerns the independent module in `v2/`. Its current API inventory is
[API.md](API.md), and protocol implementation notes are in
[V2_PROTOCOL.md](V2_PROTOCOL.md). V1 contracts are documented in [V1.md](V1.md),
[PROTOCOL.md](PROTOCOL.md) and [ARCHITECTURE.md](ARCHITECTURE.md).

## Repository layout

The root module is `github.com/sshaplygin/go-socket.io`; the v2 module is
`github.com/sshaplygin/go-socket.io/v2`. Neither imports or requires the other.
There is no `go.work`. Shared CI and development documentation live at the root.
Release and tag policy is in [CONTRIBUTING.md](../CONTRIBUTING.md#releases).

Paths and make targets below are relative to `v2/`, except shared files in `docs/`,
`.github/`, `CLAUDE.md` and `CONTRIBUTING.md`, explicit `root:` paths and paths
already starting with `v2/`. `CHANGELOG.md` below means `v2/CHANGELOG.md`.
The v2 module uses tags `v2.X.Y`; a nested module uses `v2/<dir>/vN.X.Y`.

## Execution and parallel work

V2 runtime work proceeds after its API contract checks pass and the owner
resumes development. Prerequisites require working integration and passing tests.
Parallel work uses separate write ownership; shared API changes update the
owning contract and consumer fixtures together.

| Wave | Prerequisites | Independent work / write ownership | Join gate |
| --- | --- | --- | --- |
| 2A | API design | Generic API and lifecycle contract with compile fixtures | G2: `make g2` |
| 2B | G2 and owner authorization to continue v2 | 2.1 Engine.IO (`engineio/`); 2.2 memory adapter (root `adapter.go`, against the frozen `LocalSockets`, no edit of `namespace.go`); 2.3P Socket.IO codec (`parser/`) | all three integrate against frozen contracts |
| 2C | 2B | 2.3S server/namespace runtime (root socket files, including the body of `Namespace.LocalSockets` and the server declarations of 2.3 *v1 continuity*); 2.3C client (`client/`, with the names and reconnection of 2.3 *v1 continuity*) | typed Go/Node tests, lifecycle tests (including `TestNamespaceReadiness`, 2.3S) and the *v1 continuity* acceptance pass; dispatch baseline recorded |
| 2D | 2C | one owner propagates instance loggers across runtime packages | logger precedence/isolation tests pass |
| 2E | 2D | 2.4E Engine.IO hook fire points; 2.4S Socket.IO hook fire points; 2.4O OTel bridge (`contrib/otel`) against frozen hook fixtures | all hook, span, metric and overhead checks pass |
| 2F | 2E | 2.5T conformance/framework tests; 2.5D migration/examples/docs; 2.5M T3 (`cmd/socketio-migrate`) | M3 acceptance; publication verification at tag time |
| 3A | M3 | freeze chat event schema; then server, browser/CLI and load client in separate directories | M4 single-server acceptance |
| 5L-A | M3 | freeze single-server admin observation, snapshot/watermark types, command capability and command-result contracts, session-store interface and advertised features; pin the UI release and instrumentation fixtures | 5L contract gate |
| 5L-B | 5L-A | core observers/snapshots; admin wire/auth module; memory-adapter command capability; browser tests against the pinned UI | M3A acceptance |
| 4A | M4 | freeze codec fixtures and adaptertest cases; then Redis and NATS modules independently | each passes shared conformance suite |
| 4B | 4A | cluster chat profile; mixed Go/Node Redis tests in separate test directories | M5 cluster acceptance |
| 5C-A | M5, M3A | freeze the 5C additions to the 5L contracts: the node snapshot/watermark adapter interface, the broker encoding of remote commands and the shared-store implementation contract; 5L-A signatures do not change | 5C contract gate |
| 5C-B | 5C-A | Redis/NATS capability implementations; cluster statistics and updates; mixed Go/Node fallback; cluster browser tests | integrate and run M6 acceptance |
| 6A | M6 | benchmark owner freezes versions, workload matrix, resource budgets and result schema | comparison contract and correctness checks pass |
| 6B | 6A | our-v2, existing-Go and official-Node runners in separate directories; shared load generator owned by integrator | runners produce equivalent traffic/results |
| 6C | 6B | measurements sequentially on reserved hosts; analysis/report follows complete raw results | M7 reproducibility and report acceptance |

Each join builds/tests the whole v2 module and affected child modules. Mocks
support isolated development; integration and release gates exercise real peers.
2.5M writes its own module `cmd/socketio-migrate`, its CI job (the workflow job, its
change-classifier entry and its place in `ci-ok`) and the `CLAUDE.md` layout row of the
module, and joins no earlier gate: its T1 may start during 2B or 2C, each of its goldens
using only declarations already on `master`, T2 starts once the 2.3S and 2.3C declarations it targets are on `master`,
and T3 belongs to 2F (2.5M).

## Stage 2. Socket.IO protocol v5 over Engine.IO protocol v4 (tag `v2.0.0`)

Protocol deltas are listed in [V2_PROTOCOL.md](V2_PROTOCOL.md#protocol-changes-from-v1-engineio-v4-and-socketio-v5).
Stage 2 applies to `v2/` (paths relative to that directory). New code for the `Socket` model goes to
the root files `server.go`, `namespace.go`, `socket.go`, `packet_handlers.go`,
`event.go`, `options.go`, `errors.go` and `adapter.go`; the client goes to `client/`.

### Prepared components

An `_experiments/<name>/` directory is a standalone module with its own `go.mod`, never
imported by the root module and not listed in `go.work`; it never joins `./...`. This
rule is checked by `make experiments`. Otherwise `make experiments` runs Go checks only, so a landing PR also records
the Node oracle or script checks of its module that it ran by hand. A module lands in
`master` through a fresh-branch cherry-pick PR of its owned commits (transfer rules in
issue #2; #44, #45, #54, #55 and #53 landed this way and supersede draft PRs #8, #9, #1, #6 and #7); it is later
absorbed into live packages by the PR that lands its consumer, and that PR deletes the
experiment and its `Unreleased` CHANGELOG entry (no experiment is open today; `adapter-rooms` went into
`testdata/rooms` with the 2.2 memory-adapter PR, `adapter-wire`
with the first 2.2 PR, which created `adapter/codec`, `sio5-codec` was absorbed into `parser/` by 2.3P, and
`eio4-websocket`, `ws-bench` and the `internal/eio4` entry went with the 2.1 WebSocket PR). M2 acceptance checks the deletions. A draft PR does not satisfy a gate.

| Component (source PR) | Purpose | Consumer | State in `master` and retirement |
| --- | --- | --- | --- |
| `_experiments/adapter-rooms` (#9, landed as #45) | Node memory-adapter room corpus | 2.2 conformance | absorbed into `testdata/rooms` (`TestMemoryAdapterNodeRooms`) by the 2.2 memory-adapter PR, which deleted the module |

### 2.0 Generic API and lifecycle contract

The v2 API contract is independent of v1. Carry applicable regression scenarios
into v2 fixtures. Engine.IO/parser packages remain buildable during runtime work.
The v2 application examples are built once migrated in 2.5D; Redis examples are
covered by 4b.

Implement the v2 generic API skeleton on Go 1.22 before runtime dispatch.
Positive compile fixtures cover server and client registration,
emit, ack, room broadcast, `Args2`, binary data, and the creating call with `ctx` and an
`AdapterFactory` taking `ctx` (2.2 *Readiness*); negative fixtures must reject a
wrong handler argument, ack return type or emitted payload. Keep these fixtures in
CI. Freeze the shared types consumed by parallel work: `Endpoint`, client registration
interface, `Options`, packet/argument codecs, `Adapter`, `AdapterFactory`, the namespace-creating call (2.2
*Readiness*), both hook structs and result enums. The method-signature inventory and the
acyclic package graph are published in [API.md](API.md); `make graph` checks the graph and
`go test` the fixtures. No placeholder
`any` handler, unresolved signature or TODO in these interfaces passes G2 (`make freeze`
checks it, see the *G2 record*). Runtime
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
  independently of user handlers. That reader belongs to the Socket.IO layer (2.3S on
  the server, 2.3C in the client): it keeps calling `engineio.Conn.NextReader`, which
  processes PING and PONG inside the call (no reader goroutine in `engineio`), never
  runs user code inline and never blocks on a full handler queue (overflow follows the
  2.3 *Resource limits*). A reader that stalls longer than `pingTimeout` after a PING
  (server) or `pingInterval + pingTimeout` (client) ends the session with
  `ping timeout`. A bounded worker queue per namespace socket runs
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
  session's queued outbound messages within the session ping timeout; closes
  started inside the library discard. A peer namespace DISCONNECT ends only
  that socket and keeps the session queue.
  The DoD below includes a disconnect-drain test; the v1 to v2 mapping is in 2.5.
  Application handlers must honor cancellation: Go cannot forcibly
  terminate them, and shutdown returns on its deadline even if one does not exit.
  Injected broker clients, loggers and OTel providers remain application-owned.

DoD: compile fixtures pass on Go 1.22; runtime stages add tests for request-context
independence, handler-initiated ack, ordering, concurrent close/ack/timeout, bounded
queues, graceful/forced shutdown and an application close that drains within the ping
timeout while a library close discards, all under `-race`. Include timeout of A, new
request B and late ACK A: B must remain pending until its own terminal condition.

**G2 record.** The 2A owner reviewed every open item of the skeleton. Each is either
settled, with the declaration, the fixtures and [API.md](API.md) changed together, or moved
out of the freeze to the stage that defines it; a moved item is an addition and changes no
frozen declaration. API.md (*Frozen contract*) lists the result; this table owns the reasons.

| Item | Decision | Reason | Left to |
| --- | --- | --- | --- |
| `Adapter`, `AdapterFactory`, `BroadcastOptions`, both hook structs | settled as declared (2.2, 2.4) | 2B consumers build on them; they match the roadmap text line for line | none |
| `RemoteSocket` | settled: four fields as Node's `fetchSockets`; `Handshake` omits exactly `auth` and the `authorization`, `cookie` and `proxy-authorization` headers and keeps every other key, header, `url` and `query` as Node does (they may carry credentials); `Data` nil or valid JSON, no binary | a snapshot crosses node boundaries and reaches application code; Node's own snapshot carries the three headers and `auth`, so a decoding adapter drops them too. The rest is not redacted because it is Node's own data that applications read back, a value filter would guess, and dropping more keys later changes observable snapshots; the limit is documented in the godoc and API.md | producer API for `Data` (2.3S); shared helper `socketio.RedactHandshake` (declared by 2A, written by 2.2) and conformance case (2.2, 4b) |
| `BroadcastFlags` | settled: `Local` only | volatile and compress need the queue and codec design, timeout belongs to broadcast acks, which the contract does not have; a new field is additive for keyed literals | later flags, additive |
| `Options` | settled: field names, budget names and the defaults of 2.3 *Resource limits* | the names were fixed by 2.3 and the tests pin the defaults | none |
| Payload preview redaction | moved out: the redactor type and `engineio.Options.PayloadRedactor` removed from the skeleton | the boundary hands unredacted packet bytes to a trusted component; its method set follows from the fire points and the Socket.IO classifier, which 2.4E and 2.4S own; `PayloadPreviewBytes` and `PacketInfo.Preview` stay because 2.4 names them | 2.4E, 2.4S |
| Codec contract | settled minimum: `parser.Packet`, `Arguments`, `BinaryValue` and `ArgumentCodec[T]` as value forms; values passed to a callee are borrowed, returned values owned | 2.1 relies on no `parser` type (`engineio` never imports it); 2.3P and `adapter/codec` need the value forms and nothing else | stream encoder and decoder, placeholder validation (2.3P); descriptor binding (2.3S) |
| `SocketID = Room` | settled: alias | every socket is in the room named by its ID; a defined type would need conversions in `To` and `Except` | none; it cannot be undone without a breaking change |
| `Endpoint.RequestAck` | settled: returns the raw `parser.Arguments` | the ack of Node is positional arguments; the error-first convention is applied by `AckEvent` above the endpoint | typed decoding (2.3S) |
| Result and reason domains | moved out: upgrade results, request-rejection reasons, disconnect reasons, adapter results | the roadmap does not enumerate them and the hook fields are strings; a constant added later is additive | 2.4 (`docs/OBSERVABILITY.md`) |
| `ChainHooks`, `LoggingHooks` | settled: signatures declared in both packages as in 2.4, returning nil | the signatures are fixed by 2.4; the behaviour needs the fire points | 2.4E, 2.4S |
| `Server.ServeHTTP` | settled: declared, the skeleton answers 501 | the signature is `http.Handler`, asserted by 2.1 | 2.1, 2.3S |
| Lifecycle context of `Socket` and `Namespace`, disconnect, connection callbacks, socket data (`SetData`/`Data`), descriptor codec binding | moved out | they need the runtime design; each is an added declaration | 2.3S |
| Namespace API for external adapters (local sockets, local delivery of a received broadcast) | settled: `LocalSockets` (`Deliver`, `Snapshot`) in `adapter.go` and `Namespace.LocalSockets()` in `namespace.go`, with a fixture and a negative fixture | the memory adapter is its first user and 2B writes it in parallel with the runtime of 2C, so the seam must exist before either starts; a broker adapter calls the same two methods for a received broadcast. 2A owns both declarations; 2.2 never edits `namespace.go`; 2.3S fills the body and may not change the signatures | body: 2.3S (2C) |
| Received `ServerSideEmit` (delivery to application handlers, acks of server-side emits) | moved out | no memory-adapter or 2B consumer: a single node has no peer to receive from | 4b, additively, with 2.3S |

The gate evidence is `make g2`: `make graph` (package graph), `make freeze` (`TestFrozenContract`:
none of `TODO`, `FIXME`, `proposed`, `unreviewed`, `not yet frozen`, `open before G2` or a
placeholder `any`/handler wording in API.md or in the comments of the frozen files, and no
bare `any` or `interface{}` in an exported function, method, func type, func-typed field, interface method
or struct field of the frozen files, other than the variadic payload of `ServerSideEmit`; `TestAnyUsesDetects` proves the
check fails on each of those shapes) and `go test -run
'^(TestCompileContracts|TestInventory.*)$' .`, which compiles the
positive fixtures and the negative fixtures with their recorded diagnostics, and checks that
API.md lists every exported declaration with its declared result types. CI runs `make
graph` and `make freeze` in the `lint` job and the fixtures in `go test ./...`, so `min-go`
runs them on Go 1.22 with `GOTOOLCHAIN=local`. (Later note: since PR #137 this holds for a
push to master; a pull request runs them as tests of the v2 root package, Go 1.22 included,
when it selects that package, and otherwise in `lint-v2` and `make fixtures` of `gates-v2`
when the change classifier sets `gates_v2`; see docs/API.md.)

### 2.1 Engine.IO v4 and gobwas/ws

Entry: G2. Exit: Engine.IO suite, transport/client tests and idle-connection baseline.

Reuse the prepared work that landed in `master` (#54, from the baseline's
`codex/eio4-payload` revision). The 2.1 owner integrates it after G2, preserving tests
and updating imports to the v2 layout; do not repeat completed codec work.

| Prepared component | Evidence | Remaining 2.1 integration |
| --- | --- | --- |
| `engineio/payload` (moved from `internal/eio4`) | polling codec, bounded `DecodeReader`, exact-wire `EncodeBatch`, fixtures/fuzz tests and pinned Node oracles | integrated by the polling transport (PR B): POST limits and status mapping, client batching, cancellation and deadlines, pause/upgrade lifecycle; the open packet advertises `maxPayload`, the polling POST limit (PR D1) |
| `engineio/transport/websocket` (moved from `internal/eio4` and `_experiments/eio4-websocket`) | complete-message EIO4 codec with binary/text fixtures and parser oracle; gobwas framing (masking, fragments, UTF-8, control frames, message limits, close statuses) with a pinned `ws` peer oracle | integrated by the WebSocket transport (PR C): `FrameReader`/`FrameWriter` over the codec, `ws.Dialer`, deadlines and buffer options kept; the `maxPayload` of the open packet, the v4 heartbeat and the upgrade lifecycle (PR D1); the upgrade guard and `UpgradeTimeout` (PR E1). Open: limits and timeouts as options (PRs E2a, E2b), fuzzing (PR E3) |

Preparation policy remains explicit: polling reads are byte-bounded before
buffering, then decoded as a complete batch without an additional packet-count cap.
Decoded heap can exceed wire bytes; retain `BenchmarkDecode/dense-records` and
measure amplification. Incremental decoding is deferred, not an integration gate.
Keep canonical base64/UTF-8 validation and fixtures for intentional differences from
Node: exact base64 batching, oversized-first-packet rejection and no partial decode
on invalid batches. The advertised `maxPayload` limits client POSTs, not server responses; the Go client reads a response up to `Transport.MaxPayload` and fails the session above it (docs/V2_PROTOCOL.md). Reuse
locked reference versions, recording changes when refreshed; these oracles establish
component behaviour, not full Go-server conformance. The Node oracles are outside the
required `go test ./...` set: they run by hand (`engineio/payload/testdata/README.md`,
`engineio/transport/websocket/testdata/README.md`), and `TestNodeOracle` runs the `ws`
peer against the WebSocket transport when its dependencies are installed.

- Replace the legacy `engineio/payload`/`pauser` transport integration using these
  codecs; complete the session lifecycle work rather than rewriting codecs again.
- `engineio/session`: server ping ticker and `pingTimeout` to await each pong. Add
  `maxPayload` and noop on upgrade. The session pauses polling on `5`, as the engine.io
  server does, not at `3probe` (done in PR D1, with the upgrade note of
  [V2_PROTOCOL.md](V2_PROTOCOL.md#implemented-engineio-protocol-v4)). `engineio/client`: the client uses `pingInterval+pingTimeout` to detect a missing server ping, and
  `dialer.go` sends `EIO=4` instead of the fixed `EIO=3`. `engineio/server.go`: `EIO` check, JSON errors.
  The polling GET 500 and invalid-method 400 answers, unlogged in v1, log
  `engineio: request rejected` with reasons `flush` and `bad method`; `flush` logs
  DEBUG when the session or its payload has already closed or failed, WARN otherwise.
  `docs/OBSERVABILITY.md` (2.4) owns the full v2 `reason` list.
- `engineio/transport/websocket` rewritten on `gobwas/ws`: `ws.UpgradeHTTP` hijacks the
  connection (HTTP/1.1 only); frames read with `wsutil.Reader` and written with
  `wsutil.Writer` so the `FrameReader`/`FrameWriter` contract is preserved; control
  frames handled by `wsutil.ControlFrameHandler`; one write mutex per connection;
  `CheckOrigin`, `ReadBufferSize`, `WriteBufferSize` options kept. The client
  (`engineio/client`) uses `ws.Dialer`. `gorilla/websocket` removed from `go.mod`. Decisions: `permessage-deflate` is not
  negotiated; `Transport.Proxy` is kept as an `http` CONNECT wrapper around `NetDial` (other proxy
  schemes fail the dial); `Transport.MaxPayload` (default 1 MiB, as polling) bounds one message,
  fragments together; from PR E2a `engineio.Options.MaxPayload`, when set, overrides it on the server, and otherwise the field keeps applying, always on the client; the connection's `ServeHTTP` returns at
  once, because the upgrade hijacked the connection. Both `engineio.Server` and
  `socketio.Server` assert `var _ http.Handler`; a wrapped `ResponseWriter` without
  `http.Hijacker` is answered with HTTP 501 and an `engineio: request rejected` line with
  `reason="no hijacker"`, never a panic.
- CI job runs `socketio/engine.io-protocol/test-suite` (Node) against the Go server.
- `BenchmarkIdleConnections` (10k websocket connections, RSS and goroutines) recorded
  in `CHANGELOG.md` before and after the swap (both recorded there, with the commands, from
  `engineio/idle_bench_test.go`).
- Limits, timeouts and fuzzing land in four PRs, E1 → E2a → E2b → E3. Node engine.io 6.x is the
  reference where it has an equivalent; V2_PROTOCOL records each difference and each limit
  Node does not have. Rules shared by every new option: a field of a transport
  (`polling.Transport`, `websocket.Transport`) applies on the side that uses that transport,
  and its zero means the default; an `engineio.Options` field, when non-zero, overrides the
  transport field on the server without changing the shared `polling.Default` and
  `websocket.Default`, and its zero keeps the transport's value. `Normalize` only rejects a
  negative value (it fills no default); `NewServer`, which returns no error, treats a negative
  value as zero. Each PR lists its fields in API.md, its additions under CHANGELOG *Added* and
  every behaviour change under *Changed*, and corrects the godoc, V2_PROTOCOL and PARITY text
  it makes false (for E1 the upgrade window and the code-3 cause; for E2b the heartbeat
  paragraph, whose deadline then bounds reads only).
  - **E1, upgrade guard and timeout.** The session keeps an upgrade reservation under its
    lock; `engineio.Server` takes it atomically before the transport accepts (hijacks) a
    websocket request for an existing `sid`, and releases it when the upgrade does not switch
    the session, including a rejected accept (origin check, handshake error, no hijacker) and
    an expired upgrade timeout. A websocket request with the `sid` of a session that is
    upgrading or already on websocket (upgraded, or opened there) is rejected before the
    hijack with HTTP 400, JSON code 3 and one `engineio: request rejected` record with
    `reason="bad upgrade"`. This replaces today's answers, each a behaviour change: while the
    session is upgrading, a second 101 and a concurrent probe; when it is upgraded or was
    opened on websocket, an empty HTTP 200. Node answers 101 and then closes the new
    connection while the session is upgrading or upgraded, and treats a second websocket of a
    session opened on websocket as an upgrade candidate (`_maybeUpgrade`); V2_PROTOCOL records
    both differences. `Options.UpgradeTimeout` (zero means 10 s, Node's `upgradeTimeout`)
    bounds the probe and `UPGRADE` together from the accept of the websocket connection,
    replacing the reuse of `pingTimeout` (a behaviour change: the window was `pingTimeout`, 60
    s by default); on expiry the websocket connection is closed and the session stays on
    polling, unpaused. Acceptance, `-race` tests at `engineio.Server` level, each checking at
    cleanup that every connection is closed and no library goroutine remains: two concurrent
    upgrades of one `sid` switch the session once and reject the other with code 3; a
    websocket request with the `sid` of an upgraded session, and of a session opened on
    websocket, gets code 3 and one `bad upgrade` record, while an opening websocket request
    without `sid` still succeeds; an upgrade completes after a rejected accept and after an
    expired `UpgradeTimeout`; an answered probe without `UPGRADE` closes the websocket
    connection at `UpgradeTimeout`; `NewServer` with a negative `UpgradeTimeout` behaves as
    with zero and `Normalize` rejects it.
  - **E2, limits and timeouts as options**, in two PRs. **E2a** takes `Options.MaxPayload`,
    the private transport copies (with the `MaxPayload` override and the `CheckOrigin`
    fallback) and `client.Dialer.HandshakeTimeout`, with the acceptance clauses for those.
    **E2b** takes `WriteTimeout` (transports, `Options`, the copies), the heartbeat deadline
    that bounds reads only, the close classification and `DisableCORS`, with the acceptance
    clauses for the write timeout, the 101 bound, the stalled peer, the classification, the
    read-deadline backstop, the write benchmark and the CORS switch.
    - `Options.MaxPayload` over the existing transport `MaxPayload` fields (default 1 MiB;
      Node's `maxHttpBufferSize` is 1e6 bytes, a recorded difference). It bounds a polling
      POST in wire bytes and one websocket message in data bytes, fragments together, which is
      Node's `ws` unit; the godoc of `websocket.Transport.MaxPayload` and
      `transport/parameters.go` say so. The OPEN packet advertises the limit the polling
      transport enforces.
    - `WriteTimeout`, new on both transports and in `Options` (default 10 s; Node's server has
      none; a negative transport value means none, the only way to disable it). The transport
      arms a deadline for each network write and clears it afterwards: a websocket message,
      under the connection's write lock, on both sides; the 101 response of
      `websocket.Transport.Accept` (through `HTTPUpgrader.Timeout`); a polling response body,
      around the write, on the server only (the godoc of `polling.Transport.WriteTimeout` says
      so). A deadline set through `transport.Conn.SetWriteDeadline` stays, and the earlier of
      the two applies. The Go polling client's POST stays bounded by its `http.Client`, as
      today; the Go websocket client gains the per-message deadline, a behaviour change. The
      wait for the next poll is not a write and is not bounded by it; the session's heartbeat
      deadline governs reads only. An expired write closes the session with reason `transport
      error`; an expired read deadline stays `ping timeout`. A peer that stops reading thus
      ends the session within `WriteTimeout`, also when PING and other writers are queued
      behind the stalled write. A Go client whose PONG write stalls already closes at its own
      `pingInterval + pingTimeout` timer.
    - Mechanism: `NewServer` keeps private copies of the configured `*polling.Transport` and
      `*websocket.Transport` values with the `Options` overrides applied; changing a transport
      struct, or `polling.Default.CheckOrigin` (the polling fallback, resolved into the copy),
      after `NewServer` has no effect on that server, a behaviour change; other
      `transport.Transport` implementations are used as given.
    - `client.Dialer.HandshakeTimeout` (zero means 10 s, negative means none) bounds, per
      transport tried, the dial, the websocket handshake and the wait for OPEN, a behaviour
      change. Today the websocket handshake is bounded only by
      `websocket.Transport.HandshakeTimeout` (zero by default, no bound), the websocket OPEN
      read has no bound, and the polling OPEN is bounded only by the `http.Client` timeout (1
      min for `polling.Default`, none for a custom client without one). Those bounds keep
      their meaning and apply together with `Dialer.HandshakeTimeout`; the shorter wins. The
      `Dialer` dials a private copy of a `*websocket.Transport` whose `HandshakeTimeout` is
      the shorter bound, and bounds the wait for OPEN with the connection's read deadline; for
      other `transport.Transport` implementations only the wait for OPEN is bounded. On the
      server, the 2.4E handshake result `timeout` is an expired `WriteTimeout` on the 101 or
      the OPEN write. The server has no handshake option: net/http has read the request before
      the transport sees it (the application's `http.Server.ReadHeaderTimeout` bounds that),
      and the 101 response is a write bounded by `WriteTimeout`.
    - `DisableCORS bool`, new on `polling.Transport` and in `Options` (false keeps the
      default). The v2 default stays as it is: the polling transport writes
      `Access-Control-Allow-Origin` (with `Access-Control-Allow-Credentials: true` when the
      request has an `Origin`) only when its `CheckOrigin`, or the `polling.Default.CheckOrigin`
      fallback, allows the request, and a nil check allows none; the websocket transport
      accepts by `CheckOrigin` or, when nil, by the same-origin rule. This default already
      behaves as v1 `engineio.WithCORS(false)`, under which a transport `CheckOrigin` still
      produced the headers for the origins it allowed. `DisableCORS` is stricter than that v1
      option: with it set, the server writes no `Access-Control-*` header on any polling
      response, the OPTIONS preflight included, whatever `CheckOrigin` returns, so an
      application that writes its own CORS headers in front of the server is not overridden;
      the websocket origin decision does not change. `Options.DisableCORS` reaches the
      server's private polling copy: the copy has `DisableCORS` set when either
      `Options.DisableCORS` or the transport field is true; the Go client ignores the
      field. v1 allowed every origin with credentials by default; v2 has no permissive
      default. V2_PROTOCOL records that difference; `docs/MIGRATION.md` and 2.5M report it
      (2.5).
    Acceptance, `-race`: an `engineio.Server` test with `Options.MaxPayload` set and different
    transport fields checks the advertised `maxPayload`, a polling POST 413 and a websocket
    close 1009 at the `Options` limit, on a session opened on websocket and after an upgrade;
    a test with a non-default transport field and zero `Options.MaxPayload` checks the
    transport value applies; `NewServer` with negative values behaves as with zeros and
    `Normalize` rejects them; on each transport, a peer that stops reading (a websocket peer,
    a polling client that stops reading a GET response) ends the session within `WriteTimeout`
    plus a stated margin with writers queued behind the stalled write, and a negative
    transport `WriteTimeout` disables it; a polling client that delays its next GET beyond
    `WriteTimeout`, within the heartbeat, still receives the queued packet; a transport-level
    test with a connection whose `Write` blocks shows the 101 bounded; a server that accepts
    TCP but never answers the websocket handshake, and one that never sends OPEN on each
    transport, fail `Dial` at `HandshakeTimeout` with an error identified by `errors.Is`; a
    websocket server that stops reading ends the Go client within `WriteTimeout` plus a
    margin; at `engineio.Server` level an expired write deadline closes the session as
    `transport error`, and at session level (a connection that fails after the read deadline
    while no PING is outstanding) a read failure after the deadline is `ping timeout`; the
    read-deadline backstop is pinned at transport level (a polling POST body that stalls past
    the read deadline fails and releases its handler, as
    `TestServerPostStalledBodyHitsReadDeadline` does, and a websocket counterpart). A
    websocket write benchmark shows the cost of the per-write deadline, base and head. At
    `engineio.Server` level, with `DisableCORS` set in `Options` and, separately, on the
    transport, a polling handshake, poll, POST and OPTIONS preflight carry no
    `Access-Control-*` header, both for an `Origin` that `CheckOrigin` allows and without
    `Origin`, while with the switch unset the same requests carry the headers above; a
    cross-origin websocket upgrade is accepted or refused exactly as without the switch.
  - **E3, fuzzing.** Fuzz targets `FuzzServerPost` (`transport/polling`: POST body to decoded
    packets), `FuzzNextReader` (`transport/websocket`: frames to messages to packets) and,
    after E1, `FuzzUpgradeSequence` (`engineio/session`: probe, upgrade, close and data
    packets in any order). Properties: a body or message above the limit is rejected before it
    is buffered whole; decoded packets re-encode to the same wire form or the input is
    rejected; every sequence ends with at most one switch, the session closed, every
    connection closed and no goroutine left. Each target has a committed seed corpus, and
    every reproducing input found is committed under `testdata/fuzz/<FuzzName>/`, so `go test`
    replays it. The PR reports the `-fuzztime` each target ran locally.

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
type AdapterFactory func(ctx context.Context, nsp *Namespace) (Adapter, error)
func (s *Server) Namespace(ctx context.Context, name string) (*Namespace, error) // creating call
```

`Adapter`, related types and the in-memory implementation live in root `socketio`;
`adapter/codec` (created here; its first slice, the Node Redis adapter message format over
codec-local wire types, has landed) depends on parser/wire types, never on root `socketio`. External
adapters import the root; the root never imports them. This avoids a cycle through
the `*Namespace` parameter of `AdapterFactory`. The memory adapter is the v2 default; legacy removal belongs to 2.0. The v2.0
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

*Snapshots and flags.* `RemoteSocket` follows Node's `fetchSockets` entry (`ID`, `Rooms`,
`Handshake`, `Data`). `Handshake` is a JSON object with the Node key names. The guarantee is
exactly this: it has no `auth` key and its `headers` omit `authorization`, `cookie` and
`proxy-authorization` (compared without case). Nothing else is redacted: `url`, `query`,
`address` and every other header (`set-cookie`, `x-api-key`, `x-auth-token`) pass through
and may contain credentials, for example a token in `query`. They are Node's own fields
that applications read back; a filter on values would guess, and omitting more keys later
changes observable snapshots, so it is a contract change. An application keeps secrets out
of the URL or removes them after `FetchSockets`. One helper does the omission,
`socketio.RedactHandshake(json.RawMessage) (json.RawMessage, error)`, declared in
`adapter.go` by 2A (the skeleton returns `ErrNotImplemented`) and written by this stage. It
lives in the root because the root may not import `adapter/...` while every adapter
already imports the root, so one location is reachable by all callers under the frozen
graph: the root calls it for local sockets (2.3S, in `Namespace.LocalSockets().Snapshot`) and
each adapter package calls it on a snapshot decoded from a peer, because a Node peer sends
the omitted values. `adapter/codec` decodes the wire fields only and does not call it,
since it never imports the root (`TestForbiddenEdge` pins both `. -> adapter/codec` and
`adapter/codec -> .` as forbidden, and `adapter/<name> -> .` as allowed; the
`externaladapter` fixture calls the helper from outside the root). The conformance suite (memory adapter
here, `adaptertest` in 4b for every broker adapter) decodes a peer snapshot that carries
`auth`, the three headers in mixed case, `x-api-key` and a `query` token, and asserts that
the first two groups are gone and the other two are unchanged. `Data` is the slot
of `socket.data`: nil, or valid JSON; binary values are not representable, and a producer
that cannot encode it returns the entries it can with an error. `BroadcastFlags` is `{Local bool}`; volatile, compress and timeout are later
additive fields. The adapter-facing seam is `LocalSockets`, frozen at G2: `Deliver(ctx, sid,
pkt)` queues a packet on one local socket (success counts in `LocalRecipients`) and
`Snapshot(sid)` returns its `RemoteSocket`; a namespace hands it out through
`Namespace.LocalSockets()`. Ownership: 2A declared both (`adapter.go`, `namespace.go`), this
stage writes the memory adapter in `adapter.go` and never edits `namespace.go`, 2.3S (2C)
implements the accessor body. Before the runtime exists the memory adapter is built from a
`LocalSockets` value, not from a `*Namespace`, and its conformance tests (room union,
exclusions, `LocalRecipients`, partial failure, the redacted `Snapshot` use) run against a
test double; the default factory only wraps `nsp.LocalSockets()`, so no 2B test needs a
socket. Sockets and snapshots that reach `Deliver` and `Snapshot` through a live
`Namespace` are covered by the 2C tests.

*Readiness.* The factory and namespace creation are context-aware:
`AdapterFactory` takes `ctx`, and namespace creation uses `Namespace(ctx, name)`
returning an error. The 2.0 skeleton declares both lines ([API.md](API.md)); the
experiment that held the earlier `Namespace(string) *Namespace` and `AdapterFactory
func(*Namespace)` was deleted by that PR. `Options.Adapter` carries the factory (default: the memory adapter). `NewServer`
creates no namespace, `/` included; a CONNECT to one the application has not created is
unknown. In tests *at once* means within 100 ms. 2.3S implements the server side and
owns its root test; 4b reproduces the broker cases.

- *Creation:* a namespace is created only by `Server.Namespace` and registered only when
  the factory returned it. A CONNECT never creates one: a CONNECT to a namespace that is
  not registered, including one being created, is rejected as unknown; dynamic
  namespaces (mentioned in 2.4) have no owner stage yet, and the stage that adds them defines how a
  CONNECT waits. One call decides in this order: (1) once shutdown has begun it returns
  an error matching `ErrNamespaceClosed`, also for a registered namespace, and calls no
  factory; (2) a registered namespace is returned without calling the factory; (3) a
  creation in progress is waited for, and its namespace or error returned; (4) otherwise
  the server calls `AdapterFactory` outside every lock that packet dispatch reads. Steps
  (1) and (2) ignore `ctx`; a `ctx` already done at (3) or (4) returns its error at
  once, and at (4) calls no factory. An error returned after shutdown began also matches
  `ctx.Err()` when the caller's `ctx` had ended. On a factory error nothing is
  registered, the call returns the error wrapped with `%w` and naming the namespace, and
  a later call calls the factory again. The factory context is the server's. The call's
  `ctx` bounds only its caller's wait: when it ends first, the call returns an error
  matching `ctx.Err()` and the creation goes on. A creation outlives its callers: when
  none is left and shutdown has not begun, a successful result is still registered (the
  next call returns it by step 2) and is closed by `Shutdown` or `Close` like any other,
  so an abandoned creation leaks nothing. A broker adapter returns only after the broker
  it is connected to confirmed its subscriptions, or with an error within a bound it
  documents as an option, leaving nothing of its own open. That is all the confirmation
  proves: one Redis master covers every peer, but a NATS flush does not show that other
  servers of a cluster, gateway or leafnode link have the interest, so a peer's
  broadcast right after construction can be missed there (4b `adapters/nats`).
- *Shutdown:* `Shutdown` and `Close` cancel the factory context when they begin;
  `Shutdown` waits for factory calls in progress, callers or not, until its deadline,
  `Close` does not wait. The server decides between registering and closing a returned
  adapter under the lock that sets the shutdown flag. A creation whose factory returns
  after either has begun returns an error
  matching `ErrNamespaceClosed` whatever the factory returns: the server closes a
  returned adapter exactly once (before the call returns when a caller waits) and wraps
  a factory error alongside with a second `%w`. The cancellation also reaches built
  adapters while the drain still broadcasts, so the factory context bounds only the
  factory call: an adapter derives no lifetime from it, stops any `context.AfterFunc` on
  it before returning, and lives until `Adapter.Close`.
- *Restoring:* a broker adapter is restoring from the moment it observes the loss of
  a subscription (a receive or connection error) until the broker confirms the new
  one; before it observes the loss, queries can undercount without an error, an
  accepted gap like the disconnect gaps above.
- *Queries:* cluster queries (`Sockets`, `FetchSockets`) count local sockets locally, never through the broker,
  and wait only for the peers expected to answer. With none expected they return the
  local data and a nil error at once; while restoring, or when the expected peers
  cannot be determined, the local data and an error at once.

### 2.3 Socket.IO v5 and the generic API

Parser work starts at G2; server/client runtime starts after 2B. Exit: typed
Go/Node interoperability, 2.0 lifecycle tests and dispatch benchmark baseline.

- `parser`: CONNECT payload, CONNECT_ERROR object, marker interface instead of
  `Type().Name()=="Buffer"`, `Packet` value type with lazily decoded args. The 2.0
  skeleton froze the value forms `Packet`, `Arguments`, `BinaryValue` and
  `ArgumentCodec[T]`; 2.3P added the bounded `Encode`, `Decode` and `Assembler`,
  placeholder validation, wire errors and the `JSON[T]` argument codec beside them, and
  2.3S binds a codec to a descriptor, both without changing the frozen declarations. A
  value passed to a callee is borrowed for the call; a returned value is owned by the
  caller. 2.3P is on `master` and unreleased (the M3 tags release it); the root calls none of it
  yet. What 2.3S still does with it: map `socketio.Options` to `parser.Limits`
  (`MaxEventBytes`, `MaxAttachments`, `AttachmentTimeout`), arm a timer on
  `Assembler.Deadline`, translate the parser errors into `ErrMessageTooLarge`,
  `ErrTooManyAttachments` and the `parse error` close, allocate acknowledgement IDs, and
  build `Args2` and the typed ack convention from `JSON[T]`, `Concat` and `Slice`.
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
  JSON argument; the one exception is `struct{}`, which is no argument, as a payload `T`
  and as a result `R` (*v1 continuity*, *Events without a payload*). `Args2[A, B]` expands
  to exactly two positional arguments; it is not one JSON array. Descriptors carry typed encode/decode functions; dispatch itself
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
  was requested; with or without an ack they reach the `OnError` handler (*v1 continuity*
  below) or, without one, the log; malformed protocol envelopes
  close with `parse error`. For valid known events, server and client use this table:

  | Descriptor | Incoming ACK ID | Handler and response |
  | --- | --- | --- |
  | `Event[T]` | absent | run handler; errors go to `OnError` or, without one, the log |
  | `Event[T]` | present | run handler; reply `[null]` on success or the typed error envelope; errors also go to `OnError` or the log |
  | `AckEvent[T, R]` | absent | run handler, discard result; errors go to `OnError` or, without one, the log |
  | `AckEvent[T, R]` | present | run handler; reply using the typed ack convention above; errors also go to `OnError` or the log |

  Tests cover all four combinations with success/error, both Go/Node directions and
  binary ACKs; responses are queued at most once and only when an ACK ID is present. A
  descriptor returned by `PlainAck` (*v1 continuity*) replies to a successful handler in
  the plain v1 form instead.
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
  The reader of 2.3S and 2.3C is the one reader of 2.0: it keeps calling
  `engineio.Conn.NextReader`, closes each reader before the next call, never runs user
  code inline and never blocks on a full handler queue; a full queue is an overflow
  under the policy above. Its tests show heartbeat progress while handlers block.
  Add fuzz/regression tests for invalid placeholders, incomplete binary messages,
  argument arity and limits; verify memory remains bounded with a stalled peer.
  Returned errors distinguish oversized messages, too many attachments and queue
  saturation. Document every limit's option name, unit, default and zero-value
  semantics in the 2.0 API fixture; zero selects the bounded default, not unlimited. The
  option names are `OutboundQueueGroups`, `OutboundQueueBytes`, `HandlerQueueEvents`,
  `HandlerQueueBytes`, `MaxPendingAcks`, `MaxAttachments`, `MaxEventBytes`,
  `AttachmentTimeout`, `MaxConcurrentConnects` and `ConnectTimeout`, with `AckTimeout`
  above; G2 froze them.
- `BenchmarkEventDispatch` (root) is added with the new model, so stage 2.4 has a real
  baseline.
- Rewrite `Client` on the same generic API with websocket over `gobwas/ws`. This item delivers the v2 client; the v1 client is an independent implementation. Its names, callbacks and reconnection are fixed by *v1 continuity* below.
- 2.3S implements the server side of 2.2 *Readiness*. Its root test is
  `TestNamespaceReadiness`, with the subtests `R1` to `R9` below; the 2C join gate runs
  `go test -race -count=1 -json -run '^TestNamespaceReadiness$' .` and requires a pass
  event for each, none skipped. Each subtest (each variant of R2) builds its own server.
  The fake factory counts its calls, records its context and the time of each return, and
  returns a fake adapter that counts its `Close` calls. The *settled reading* is taken
  100 ms after the later of the factory's last return and the last `Close` or `Shutdown`
  return of the subtest; at it every adapter the factory returned has been closed exactly
  once, registered or not. A subtest states any earlier reading.
  - R1: a factory held until its context ends, then returning an adapter at once: `Close`
    during the call returns at once and the call's error matches `ErrNamespaceClosed`; the
    count is 1 when the call returns. A variant whose factory returns `ctx.Err()` instead
    returns an error that also matches `context.Canceled`.
  - R2: a factory returning an adapter 300 ms after its context ends, in two variants
    (the caller waits; every caller's `ctx` cancelled first), each ending the server in
    three runs of its own: `Shutdown` with a 1 s deadline does not return before the
    factory does, with a 20 ms deadline it returns before it, and `Close` returns before
    it. With the caller waiting, its call returns `ErrNamespaceClosed` only after the
    factory returned, and the count is then 1. With every caller gone, each call has
    returned an error matching its `ctx.Err()` at once, and the count read 100 ms after
    the end began is 0; the settled reading is 1.
  - R3: while `Shutdown` drains (a handler blocks it) and after `Close`, calls for a
    registered namespace, for an unregistered one and for one whose creation was started
    before and is still held (the test releases that factory last) return
    `ErrNamespaceClosed` at once with no further factory call; after `Close` a cancelled
    `ctx` also matches `context.Canceled`.
  - R4: a factory error comes back matching the error and naming the namespace; the next
    call calls the factory again.
  - R5: a done `ctx` returns the registered namespace; for an unregistered one it returns
    its error without calling the factory; while a creation is held, an already-cancelled
    `ctx` returns an error matching `context.Canceled` at once, the creation goes on and a
    later call with a live `ctx` returns its namespace after one factory call.
  - R6: eight concurrent creations of one namespace under `-race`, the factory held for
    100 ms, call it once and get the same namespace; the count is 0 before the test's
    `Close`.
  - R7: on a fresh server with no creating call, the factory count is 0 after `NewServer`
    and a CONNECT to `/` and to a never-created name is answered CONNECT_ERROR at once with
    the count still 0. While a creation is held, a CONNECT to its namespace is answered
    CONNECT_ERROR at once and calls no factory.
  - R8: with every caller's `ctx` cancelled while the factory is held, each call returns
    an error matching its `ctx.Err()` at once and the factory's context stays live; the
    factory then returns an adapter, the next call returns that namespace without a
    second factory call, and the count is 0 before the test's `Close`.
  - R9: a handler blocks the drain of `Shutdown` until the recorded factory context is
    done, then broadcasts to a room: the fake adapter receives that `Broadcast`, and its
    count, read in the handler right after the broadcast was delivered, is 0; the settled
    reading is 1.
- **v1 continuity.** No frozen declaration changes for v1 users ([API.md](API.md#frozen-contract)).
  The gap to v1 is closed additively by the declarations below, which keep v1 names where
  the v2 meaning allows, together with `docs/MIGRATION.md` (2.5D) and the migration tool
  (2.5M). A compatibility package with v1 signatures is not part of M3 and is reconsidered
  after M3. Each declaration lands with its API.md inventory row, a positive compile
  fixture and godoc; a value is carried by a type parameter, never by a bare `any` or
  `interface{}` (`TestFrozenContract` scans every root file); none adds a package-graph edge
  or imports `reflect`. Server side, 2.3S, in the root files listed under Stage 2:

  ```go
  func (n *Namespace) OnConnect(func(ctx context.Context, s *Socket) error) error
  func (n *Namespace) OnDisconnect(func(ctx context.Context, s *Socket, reason string)) error
  func (n *Namespace) OnError(func(ctx context.Context, s *Socket, err error)) error
  func (n *Namespace) RoomLen(ctx context.Context, room Room) (int, error)
  func (n *Namespace) Sockets(ctx context.Context, rooms ...Room) ([]SocketID, error)

  func (s *Socket) Context() context.Context
  func (s *Socket) Connected() bool
  func (s *Socket) Disconnect() error
  func (s *Socket) Close() error
  func (s *Socket) Handshake() Handshake
  func (s *Socket) RemoteHeader() http.Header
  func (s *Socket) RemoteAddr() net.Addr
  func (s *Socket) LocalAddr() net.Addr
  func (s *Socket) URL() url.URL
  func (s *Socket) To(rooms ...Room) BroadcastOperator
  func (s *Socket) Rooms() []Room
  func (s *Socket) LeaveAll() error

  type Handshake struct {
      Headers http.Header
      Time    time.Time
      Address string
      XDomain bool
      Secure  bool
      URL     url.URL
      Query   url.Values
      Auth    json.RawMessage
  }

  func SetData[T any](s *Socket, v T)
  func Data[T any](s *Socket) (T, bool)

  func (s *Server) Count() int

  func (e Event[T]) PlainAck() Event[T]
  func (e AckEvent[T, R]) PlainAck() AckEvent[T, R]
  ```

  - *Identity and connect order.* A namespace CONNECT creates one `*Socket`. Every
    middleware, `OnConnect`, event handler, `OnError` and `OnDisconnect` call for that
    connection receives the same pointer, so an application can key a map with it from
    `OnConnect` to `OnDisconnect`; a later CONNECT of the namespace, on the same session or
    another, creates a new one. The CONNECT task (*Resource limits*: counted by
    `MaxConcurrentConnects`, bounded by `ConnectTimeout`, off the reader) runs the
    middleware in registration order, then `OnConnect`, then queues the CONNECT ack. A
    non-nil error from either is answered with a CONNECT_ERROR whose `message` is
    `err.Error()`, as v1 rejects from `OnConnect`; Node's `connection` event cannot reject,
    so `Use` stays the Node-style gate. When `ConnectTimeout` expires first, the
    CONNECT_ERROR is queued at the deadline with the message of the context's error, and a
    later return of the middleware or `OnConnect` is ignored (its slot is held until it
    returns, *Resource limits*). A rejection, an expired `ConnectTimeout` or a session lost
    during the task cancels `s.Context()` and removes the socket from the adapter; a lost
    session gets no CONNECT_ERROR.
  - *Adapter membership.* While the middleware runs, the socket is in no room: no broadcast
    selects it and `LocalSockets().Snapshot` reports it absent. Once the middleware has
    passed, it joins the room named by its ID before `OnConnect` runs, as a Node socket
    joins it before its `connection` handlers run. From then on, while its CONNECT ack is
    still pending, broadcasts, `Deliver` and `Snapshot`, and therefore `Sockets`, `RoomLen`
    and `FetchSockets`, see it and every room it joins; a rejection removes it. When the
    socket ends, it leaves every room, its ID room included, and `Snapshot` reports it
    absent, before `OnDisconnect` runs, as v1 does and as Node does before its `disconnect`
    event. Inside `OnDisconnect`, `Rooms` is empty, `RoomLen` and `Sockets` exclude the
    socket, `Leave` and `LeaveAll` return nil with no effect, `Join` returns an error
    matching `ErrSocketClosed`, and `To` still broadcasts to the namespace without it.
  - *Packets before the CONNECT ack.* From the creation of the `*Socket` until its CONNECT
    ack is queued, every packet queued on it from any goroutine (an emit, `SendPacket`,
    `RequestAck`, a broadcast or adapter `Deliver` that selects it through a room joined in
    `OnConnect`) waits behind the CONNECT ack in submission order, counts toward the
    outbound budgets and is written after the CONNECT packet. On rejection those packets are
    discarded, as a library close discards; an emit that already returned stays returned,
    and a pending `RequestAck` or `EmitWithAck` ends with `ErrSocketClosed`. An `EmitWithAck`
    awaited inside `OnConnect` cannot be answered before the CONNECT ack, so it ends at the
    deadline of the `OnConnect` context; the godoc says so.
  - *Contexts.* `Socket.Context()` exists from the creation of the `*Socket`, derives from
    the session context (2.0) and is cancelled once when the socket ends: a rejected
    CONNECT, a namespace DISCONNECT from either side, `Disconnect`, `Close`, the session's
    close, or the server's `Shutdown` or `Close`. `Connected()` reports
    `s.Context().Err() == nil`, so it is true inside middleware and `OnConnect` unless the
    socket was already lost. The `ctx` of middleware and `OnConnect` derives from
    `s.Context()` with the `ConnectTimeout` deadline and ends with the CONNECT task. The
    `ctx` of an event handler (descriptor or `OnRaw`) carries the values of `s.Context()`
    and the values of the context the 2.4 `EventStart` hooks return, without that context's
    deadline or cancellation, and is done exactly when `s.Context()` is done: it has no
    deadline of its own and is not cancelled when the handler returns, so work a handler
    starts may use it for the socket's lifetime. `OnDisconnect` gets the values of
    `s.Context()` without its cancellation (`context.WithoutCancel`); `OnError` gets the
    `ctx` of the operation that failed.
  - *Callbacks.* `OnConnect`, `OnDisconnect` and `OnError` each hold one handler per
    namespace: a second registration returns an error, as `Handle` does for a duplicate
    event, and a registration after `Shutdown` or `Close` began returns an error matching
    `ErrNamespaceClosed`. Each socket captures the three handlers when its CONNECT task
    starts, so a registration applies only to sockets whose CONNECT task starts after it
    returns; an `OnDisconnect` registered later is not called for sockets already connected.
    No callback runs on the reader. When a socket ends, events in its handler queue that
    have not started are discarded: no handler runs for them and no ACK reply is queued.
    `OnDisconnect` runs once for each socket whose CONNECT ack was queued, on that socket's
    worker queue after the handler running at the end returned, with a 2.4 disconnect
    reason; it is not called for a rejected CONNECT. `Server.Shutdown` waits, within its
    deadline, for the `OnDisconnect` calls of the sockets it ends, as v1 `Server.Close`
    waited for them; `Server.Close` does not wait. 2.3S amends the `Shutdown` godoc, in
    wording only, to state that wait. `OnError` runs on
    the socket's worker queue, or for an `OnConnect` error in the CONNECT task after the
    CONNECT_ERROR is queued, always with the socket concerned (never nil). It receives
    exactly the failures marked *yes* below, after any reply is queued; a failure it
    receives lowers its 2.4 record to at most DEBUG, and every other record stays WARN:

    | Failure | ACK ID | Reply | `OnError` | 2.4 record |
    | --- | --- | --- | --- | --- |
    | handler error (descriptor or `OnRaw`) | absent | none | yes | `socketio: handler error` |
    | handler error | present | the error envelope (*Typed ack errors*) | yes | `socketio: handler error` |
    | decode failure of a known event | absent | none | yes | `socketio: unhandled error` |
    | decode failure of a known event | present | `invalid_payload` envelope | yes | `socketio: unhandled error` |
    | unknown event without `OnRaw` | absent | none | yes | `socketio: unhandled error` |
    | unknown event without `OnRaw` | present | `unknown_event` envelope | yes | `socketio: unhandled error` |
    | a handler result that cannot be encoded | present | `internal_error` envelope | yes | `socketio: unhandled error` |
    | `OnConnect` error | n/a | CONNECT_ERROR | yes | none |
    | middleware rejection | n/a | CONNECT_ERROR | no | none |
    | an error returned to its caller (an emit's encoding error, `ErrTooManyPendingAcks`, an ack timeout) | n/a | none | no | none; the caller has it |
    | outbound or handler queue overflow | n/a | the session or socket closes | no; `OnDisconnect` gets the reason | `socketio: unhandled error` |
    | `Data` that cannot be encoded (*Data*) | n/a | none | no | `socketio: unhandled error` |
    | CONNECT to an unknown namespace | n/a | CONNECT_ERROR | no (no socket) | `socketio: unhandled error` |

    The client `OnError` (2.3C) receives the same rows for its own handlers, plus a
    CONNECT_ERROR from the server; the end of a session is not an error on either side.
  - *Accessors.* `Disconnect` ends this namespace socket with a DISCONNECT; `Close` sends a
    DISCONNECT for every namespace socket of its Engine.IO session and closes the session,
    as v1 `Conn.Close` does. Both are application closes: before they return, they cancel
    the `Context()` of every socket they end and remove it from the adapter, so
    `Connected()` is false right after; they do not wait for the drain, which continues as
    2.0 states, nor for `OnDisconnect`, which runs after the running handler returned (the
    socket's queued events are discarded, *Callbacks*), so a call from a handler cannot
    deadlock. Both are idempotent and return nil once the socket
    has ended. `Server.Shutdown` and `Server.Close` send no namespace DISCONNECT: they end
    each session at the transport, as v1 `Server.Close` and Node's `server.close()` do, so
    a client that reconnects after a lost session (2.3C, Node) reconnects to a restarted
    server.
    `Handshake` returns an owned copy: `Headers` of the handshake request, with `Host` set
    from the request's `Host` field, where `net/http` keeps it, `Time` of the
    socket's creation, `Address` the request's remote address with its port, `XDomain` a
    non-empty `Origin`, `Secure` TLS, `URL` the handshake URL (unchanged by an upgrade),
    `Query` its query (a Socket.IO 5 CONNECT carries none), and `Auth` the CONNECT auth
    payload of this namespace (nil without one). It is the application's own data and is
    not redacted; the local `RemoteSocket.Handshake` snapshot is produced from it with
    Node's key names and value forms and passes through `RedactHandshake` (2.2): `headers`
    an object of lower-case names with string values, `host` included, repeated values
    joined as Node's `IncomingMessage.headers` joins them (`set-cookie` alone is an array
    of strings, as in Node); `time` `Time` in the server's local zone in the
    form of JavaScript's `Date.prototype.toString`, `Mon Jan 02 2006 15:04:05 GMT-0700`
    followed by the zone in parentheses (Go's abbreviation where Node writes the long name);
    `address` the remote IP without the port, as Node's `remoteAddress`; `xdomain` and
    `secure` booleans; `issued` `Time` in Unix milliseconds, a number; `url` the request
    target as received, path and query (Node's `req.url`), not an absolute URL; `query` an
    object of the handshake query, a single value as a string and a repeated key as an array
    of strings; `auth` omitted by `RedactHandshake`. `RemoteHeader` and `URL`
    return copies of those fields; `RemoteAddr` and `LocalAddr` are the addresses of the
    session's current transport connection. `To(rooms...)` is
    `n.To(rooms...).Except(s.ID())`: it excludes the sender, as v1 `Conn.To` does. `Rooms`
    returns an owned copy of `Adapter.SocketRooms(s.ID())`; `LeaveAll` leaves every room
    except the one named by the socket's ID.
  - *Data.* `SetData` stores the Go value, replacing the previous one; `Data[T]` returns it
    when it is a `T` (a type assertion; false when nothing is stored or the type differs).
    Both are safe for concurrent use. Nothing is copied or encoded when the value is
    stored: `LocalSockets().Snapshot` encodes it with `encoding/json` into
    `RemoteSocket.Data` on every call, on the caller's goroutine. The memory adapter uses
    `Snapshot` as the presence check of `Sockets` (2.2), so each `RoomLen`, `Sockets` and
    `FetchSockets` encodes the `Data` of every selected socket and runs its `MarshalJSON`;
    the godoc of `SetData` says so. `Snapshot` has no error result (frozen): for a value
    that cannot be encoded it returns the snapshot with nil `Data` and true, the memory
    adapter's `FetchSockets` returns that entry with a nil error, and the encoding error is
    logged as `socketio: unhandled error` (WARN, keys `sid`, `nsp`, `err`) once per stored
    value. The 2.2 rule that a producer returns the encodable entries with an error binds
    producers that encode `Data` themselves, such as a broker adapter encoding it for the
    wire, and not producers that pass `Snapshot` output through, as the memory adapter's
    `FetchSockets` does. 2.3S amends the `RemoteSocket` godoc, in wording only with the
    declaration unchanged, to say that a nil `Data` in a local snapshot may also mean an
    encoding failure, which is logged. A value the application
    mutates after `SetData` (a pointer, as v1 `SetContext` users keep) must therefore be safe
    to encode concurrently, for example through its own `MarshalJSON` under its lock. v1's
    `Conn.Context()` user value maps to `Data`; `Socket.Context()` is the lifecycle context.
  - *Counts.* `RoomLen` is the number of IDs `Adapter.Sockets(ctx, []Room{room})` returns,
    and `Sockets` returns `Adapter.Sockets(ctx, rooms)`: the union of the rooms, every
    socket of the namespace when none is given. Both are cluster-wide and follow the
    partial-data rule of 2.2. `Server.Count` is the number of Engine.IO sessions of the
    server open at the call (handshake accepted, not yet closed) on every transport; it
    does not count namespace sockets.
  - *Events without a payload.* A descriptor whose payload type `T` is `struct{}` carries
    no argument: the default codec 2.3S binds to it encodes `struct{}{}` as an empty
    argument list, so its `Emit` sends `["name"]`, and decodes any argument list by
    ignoring it, as a v1 handler without payload parameters ignores the arguments it
    receives. The same holds for a result type `R` of `struct{}`: its success reply is
    `[null]` under the error-first convention, the reply of an `Event` with an ACK ID, and
    `[]` under `PlainAck`, and `EmitWithAck` ignores the result arguments of a success
    reply. Every other `T` keeps the arity of its codec: `parser.JSON` requires exactly
    one argument and `Args2` two, where v1 zero-filled a missing argument and ignored extra
    ones.
  - *Plain acknowledgements.* `PlainAck` returns a copy of the descriptor, same name and
    types, whose acknowledgement keeps the wire of a v1 untyped handler. On success an
    `AckEvent` replies with its result arguments alone (`[r]`, two arguments for `Args2`)
    and an `Event` with an ACK ID replies `[]`, an ACK without arguments as a v1 handler
    without a return value sends. On failure it replies as v1 does for a type mismatch with
    an ACK ID: the one-argument error envelope of the typed convention,
    `[{"code": "invalid_payload", "message": ...}]` for a decode failure and the typed codes
    for a handler error (`internal_error` for an unexpected one). A Node caller's
    `(value) => ...` callback thus receives the result on success and the envelope object on
    failure. `EmitWithAck` of a plain descriptor, on server and client, reads a reply of
    exactly one argument that is a JSON object whose only members are the strings `code`
    and `message` as that error (the error value the error-first descriptor returns for the
    same envelope) and any other reply as the result; a result type whose JSON form is such
    an object cannot be told apart, and its godoc directs it to the error-first convention,
    which stays the default and the one every v2 example uses.

  Client side, 2.3C, in `client/`:

  ```go
  func New(addr string, opts ...Option) (*Client, error)
  func (c *Client) Connect(ctx context.Context) error
  func (c *Client) Close() error
  func (c *Client) OnConnect(func(ctx context.Context, cl *Client) error)
  func (c *Client) OnDisconnect(func(ctx context.Context, cl *Client, reason string))
  func (c *Client) OnError(func(ctx context.Context, cl *Client, err error))
  func (c *Client) OnReconnectAttempt(func(attempt int))
  func (c *Client) OnReconnect(func(attempt int))
  func (c *Client) OnReconnectError(func(err error))
  func (c *Client) OnReconnectFailed(func())
  ```

  - *Names.* The options keep the v1 names: `WithPath`, `WithQuery`, `WithHeader`,
    `WithTimeout`, `WithWriteBufferSize`, `WithCloseTimeout`, `WithLogger`,
    `WithTransports`, `WithMaxMessageSize`, `WithReconnection`, `WithReconnectionAttempts`,
    `WithReconnectionDelay`, `WithReconnectionDelayMax` and `WithRandomizationFactor`, plus
    `WithAuth(json.RawMessage)`, the CONNECT auth payload sent on every CONNECT,
    reconnections included. Their parameter types follow the v2 packages: `WithTransports`
    takes v2 `transport.Transport` values, and the others keep their v1 types.
    `WithPerMessageDeflate` is not provided (*Out of scope*). Each option keeps its v1
    meaning and default, and its godoc names the v2 setting it controls, with these
    differences: `WithTimeout` bounds the whole dial up to the Engine.IO OPEN, zero meaning
    20 s and a negative value no bound, as in v1, and the dialer's per-transport
    `HandshakeTimeout` (default 10 s, 2.1) applies within it; `WithCloseTimeout` bounds the
    drain of `Close`, but 2.0 bounds an application close by the session ping timeout, so
    the smaller of the two applies and zero selects the ping timeout, not v1's one minute.
    `WithMaxMessageSize` sets the transports' `MaxPayload` and `WithWriteBufferSize` the
    outbound group budget, with v1's defaults (1 MiB, 64). The path of `addr` selects the
    namespace, as in v1. `*Client` implements `socketio.ClientRegistration`, so
    `HandleClient`, `Emit`, `EmitWithAck` and plain descriptors work on it; v1's untyped
    `Emit` and `OnEvent` map to descriptors.
  - *Connect.* `Connect(ctx)` dials, sends the CONNECT and returns nil once the CONNECT ack
    arrived, where v1 returned once the CONNECT was queued; `OnConnect` runs after that,
    concurrently with the caller. `ctx` and `WithTimeout` bound the dial and the wait for
    the ack, whichever ends first; cancelling `ctx` after `Connect` returned has no effect.
    A CONNECT_ERROR is returned as an error that carries the server's `message`, and
    `OnError` receives it too; the client then closes as `Close` does: its session ends,
    emits queued for it are discarded with their pending acks ending in `ErrSocketClosed`,
    and no reconnection starts. A failed first dial returns its error and starts no
    reconnection, as in v1 (Node's client retries it). `Connect` on a connected client
    returns nil without dialing, and concurrent calls share one attempt and its result.
    `Connect` during a reconnection, in a backoff or a dial, neither dials nor shortens the
    backoff, as Node's `socket.connect()` does nothing while its manager reconnects: it
    waits for the loop and returns nil at the next CONNECT ack, the CONNECT_ERROR that ends
    the loop, an error matching `ErrSocketClosed` when `Close` or the attempt limit ends it,
    or the error of `ctx` when `ctx` ends first, which leaves the loop running. A call after
    `Close`, after a CONNECT_ERROR, after an `OnConnect` error or after `OnReconnectFailed`
    starts a new connection with the same options and callbacks, as in v1.
  - *Callbacks.* They are setters, as in v1: a later call replaces the handler and nil
    removes it. `OnConnect` runs off the reader after the CONNECT ack, so `EmitWithAck`
    inside it completes; event handlers start only after it returned nil, and events
    received meanwhile wait in arrival order and are discarded when it fails; a non-nil
    error goes to `OnError` and closes the client as `Close` does, with no reconnection.
    `OnDisconnect` runs once per namespace connection that reached `OnConnect`, with a 2.4
    disconnect reason; it is not called after an `OnConnect` error, as the server does not
    call it for a rejected CONNECT. `OnError` receives what the server
    *Callbacks* table lists for the client. The `ctx` of the callbacks and of the handlers
    is done when the namespace connection ends, as on the server.
  - *Reconnection*, part of 2.3C by M3, with v1's options and defaults, which are those of
    Node's `socket.io-client` 4: on, unlimited attempts, delay 1 s, maximum 5 s,
    randomization 0.5. As in v1, a non-positive attempt limit means unlimited, a factor of
    0 selects the 0.5 default, and a negative factor or one above 1 disables jitter. After
    the loss of a session that reached the CONNECT ack, caused neither by `Close` nor by a
    server namespace DISCONNECT, the client waits and then dials, for attempts `n = 1, 2,
    ...`: the base delay is `delay·2^(n-1)`; with a factor `f` in (0, 1] one uniform sample
    chooses a deviation `floor(sample·f·base)` and whether it is subtracted or added, as v1
    and Node's `backo2` do; the result is then capped at the maximum, so no delay exceeds
    it. The growth saturates at the maximum for every `n`: it is computed without an
    integer shift that can overflow `time.Duration` (v1 grows a float and caps a NaN or
    infinite value), so a large `n` gives exactly the maximum and never a negative or zero
    delay. `OnReconnectAttempt(n)` runs before dial `n`, `OnReconnectError` after each failed
    dial, `OnReconnect(n)` after the engine opens and before CONNECT is sent, and
    `OnReconnectFailed` once after the failed dial that reaches the attempt limit; the
    numbers start at 1, as in v1. After a reconnection the CONNECT carries the `WithAuth`
    payload and `OnConnect` runs again with a new socket ID. A CONNECT_ERROR answering a
    reconnection's CONNECT goes to `OnError`, is not retried and closes the client as
    `Close` does, as on the first `Connect`; `OnReconnectFailed` does not run for it.
    Pending acks end with `ErrSocketClosed` at the loss (2.0). An emit made while a
    connection is expected, before the first CONNECT ack or during reconnection, is queued
    within the outbound budget and sent after the next CONNECT ack, as v1 queues
    pre-connect events; an emit that would exceed that budget returns `ErrWriteBufferFull`
    and is not queued, and the client and the emits already queued stay as they are, since
    there is no session to close. The client is *ended* after `Close`, a CONNECT_ERROR, an
    `OnConnect` error, a failed first dial, `OnReconnectFailed`, a server namespace
    DISCONNECT, or the loss of a session while reconnection is off: entering that state
    discards the queued emits and ends their pending acks with `ErrSocketClosed`, and until
    the next `Connect` an emit or `EmitWithAck` returns an error matching `ErrSocketClosed`
    at once and queues nothing. The reconnection loop and its callbacks run on one
    goroutine of the client, never on the reader; `Close` during a backoff returns at once.
    `Close` is idempotent: on a client that never connected or has ended, and on a repeated
    call, it returns nil with no effect.

  Acceptance, part of the 2C join, under `-race` with the goroutine check at cleanup of the
  root and client packages. Server: the same `*Socket` reaches middleware, `OnConnect`, a
  handler, `OnError` and `OnDisconnect`, and a new CONNECT of the namespace gets a new one;
  an `OnConnect` error reaches a `socket.io-client@4` caller as `connect_error` with its
  message and reaches `OnError`, a middleware rejection does not reach `OnError`, and
  `OnDisconnect` is called for neither; an `OnConnect` that ignores its context past
  `ConnectTimeout` gets the CONNECT_ERROR at the deadline; an emit, a `SendPacket` and a
  broadcast to a room joined in `OnConnect`, sent from another goroutine while `OnConnect`
  blocks, reach the client after its CONNECT ack in submission order, none reaches it when
  `OnConnect` then fails, and an `EmitWithAck` queued then ends with `ErrSocketClosed`;
  `RoomLen` counts a socket whose `OnConnect` blocks after joining the room and does not
  count one still in middleware; heartbeat and ack progress continue while `OnConnect`,
  `OnDisconnect` and `OnError` block; a second registration of each callback returns an
  error, one after `Shutdown` began an error matching `ErrNamespaceClosed`, and an
  `OnDisconnect` registered after a socket connected is not called for it; a handler's
  `ctx` is live after the handler returned and done after the socket disconnected, carries
  a value an `EventStart` hook set and is not cancelled by that hook's cancel; inside
  `OnDisconnect`, `Rooms` is empty, `RoomLen` excludes the socket and `Leave` returns nil;
  `Disconnect` called from a handler returns before `OnDisconnect` runs, `Connected()` is
  false right after it returns, and `OnDisconnect` runs after that handler returned;
  events queued behind a blocking handler when `Disconnect` is called never run and get no
  ACK reply although they requested one; `Server.Shutdown` sends no namespace DISCONNECT,
  and with a 1 s deadline returns after an `OnDisconnect` that blocks 100 ms has returned,
  while `Server.Close` returns before it; `Handshake` returns the request's
  headers, URL, query and address and the CONNECT `auth`, which the
  `LocalSockets().Snapshot` handshake omits; for one polling handshake with a repeated
  query key, served once by the Go server and once by Node `socket.io` 4, the two
  snapshot handshakes have the same keys apart from `auth`, which only the Node snapshot
  has, equal `url`, `query`, `xdomain` and `secure`,
  the same header names, an `address` equal after removing Node's `::ffff:` prefix, a
  numeric `issued`, and a `time` that Node's `Date.parse` reads as `issued` truncated to the
  second; changing the values returned by
  `Handshake`, `RemoteHeader` or `URL` does not change a later call; `RemoteAddr` and
  `LocalAddr` match the test connection; a pointer stored with `SetData` and mutated
  afterwards appears mutated in `LocalSockets().Snapshot`, `Data[T]` with another type
  returns false, and a value whose `MarshalJSON` fails gives a snapshot with nil `Data` and
  true, a `FetchSockets` entry with a nil error and one `socketio: unhandled error` record;
  `RoomLen`, `Sockets`, `Rooms`, `LeaveAll`, `To` (sender excluded) and `Server.Count`
  match a known topology; `Disconnect` and `Close` drain as 2.0 states; each *yes* row of
  the `OnError` table, with and without an ACK ID, reaches `OnError` and lowers its record
  to DEBUG, the record stays WARN without a registered `OnError`, and a handler-queue
  overflow reaches `OnDisconnect` and not `OnError`; the four rows of the descriptor table
  pass for plain descriptors, success and failure, both Go/Node directions, binary acks
  included, a Node `(value) => ...` callback receives `invalid_payload` for a payload of
  the wrong type, and a Go `EmitWithAck` of a plain descriptor returns the peer's envelope
  as its error; a `NewEvent[struct{}]` handler runs for a Node `socket.emit('typing')`
  without arguments and for one with an extra argument, and its `Emit` reaches a Node
  listener with no argument. Client: `Connect` returns after the CONNECT ack, returns a
  CONNECT_ERROR as its error carrying the server's message (`OnError` receives it too)
  without reconnecting, returns the error of a failed first dial without starting
  reconnection, and connects again after `Close`; `Connect` on a connected client returns
  nil without a handshake, and eight concurrent `Connect` calls on a new client all return
  nil while the server counts one Engine.IO handshake; `Connect` during a reconnection backoff
  returns nil after the reconnection's CONNECT ack with no handshake of its own, and
  returns its `ctx` error when `ctx` ends first while the loop goes on; an emit made before
  the first `Connect` arrives after the CONNECT ack; an emit made while disconnected that
  exceeds the outbound budget returns `ErrWriteBufferFull` while the emits queued before it
  arrive after the next CONNECT ack; after a server namespace DISCONNECT, and after a
  session loss with `WithReconnection(false)`, an emit and an `EmitWithAck` return an
  error matching `ErrSocketClosed` at once; a later callback setter replaces the earlier one
  and nil removes it; an `OnConnect` error closes the client without reconnection and without an
  `OnDisconnect` call; the handler of an event the server emits right after its CONNECT
  ack starts only after a blocking `OnConnect` returned, and never runs when that
  `OnConnect` returns an error; after the
  server is stopped with `Shutdown` and started again on the same address, the Go client
  reconnects with `OnReconnectAttempt(1)`, `OnReconnect`, a CONNECT that carries the
  `WithAuth` payload and a second `OnConnect`, and an emit made while it was disconnected
  arrives; a CONNECT_ERROR answering a reconnection reaches `OnError`, ends the loop with
  no further dial and no `OnReconnectFailed`, and a later emit returns an error matching
  `ErrSocketClosed`; `OnReconnectFailed` runs once after the attempt limit; the delay
  function, tested directly, returns exactly `min(max, delay·2^(n-1))` for a negative
  factor and for a factor above 1, treats 0 as 0.5, and for a factor `f` in (0, 1] returns
  a value in `[min(max, base·(1-f)), min(max, base·(1+f))]` for every sample, and returns
  exactly the maximum at `n = 64` and `n = 1000` for each factor; `Close` during a backoff
  returns at once and leaves no goroutine, and `Close` on a new client, on an ended one and
  called twice returns nil.
- Example migration is owned by 2.5D after runtime and observability gates pass.

### 2.4 Observability

Depends on 2.1 (server ping ticker, close reasons), 2.2 (`Adapter`) and 2.3 (`Socket`
context, typed events, `AckTimeout`). Tracing and metrics go through two hook structs in
the root module with no external dependency; the OpenTelemetry bridge is the separate
module `contrib/otel`. `LoggingHooks` produces the session and namespace boundary
records described by the [v1 logger contract](../logger/logger.go).
Every hook must have an exercised fire point and a corresponding log
record; the coverage test verifies both. The Engine.IO hook types are defined in
`engineio/hooks.go`:

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

The 2.0 skeleton declares `ChainHooks` and `LoggingHooks` of both packages with these
signatures and returns nil (no observer); 2.4 supplies the behaviour. Result and reason
strings the roadmap does not enumerate (upgrade results, request-rejection reasons,
disconnect reasons, adapter results) are defined by 2.4 in `docs/OBSERVABILITY.md` and
added as constants; the hook signatures stay strings.

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
    EventStart      func(ctx context.Context, e EventInfo) context.Context // values reach the handler ctx
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
payload redactor. The redactor type and its `engineio.Options` field are defined and added
by 2.4E, because the boundary hands unredacted packet bytes to a trusted component and
follows from the fire points; the 2.0 skeleton declares only `PayloadPreviewBytes` (0 to
256) and `PacketInfo.Preview`. Callback retention rules and disabled-capture behaviour are tested.
Keep preview collection disabled when no redactor is configured; enabling TRACE
alone never enables v2 payload capture; v1 never logs payloads.

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
  adapters call paired hooks through `Namespace.Hooks()`. Pass the values of the
  context returned by `EventStart` to the handler, without its deadline or cancellation
  (2.3 *v1 continuity*, *Contexts*).
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

**Logging and overhead.** The inline boundary records are removed where a
hook now exists, and `LoggingHooks` emits the same messages and keys. It also emits the
records not emitted by v1, with these keys:

| Layer | Message | Level | Keys |
| --- | --- | --- | --- |
| engineio | `engineio: upgrade start`, `engineio: upgrade end` | DEBUG | `sid`, `from`, `to`, `err` |
| engineio | `engineio: packet` | TRACE | `sid`, `dir` (`in`, `out`), `pkt_type`, `frame`, `bytes`, `payload` |
| engineio | `engineio: ping`, `engineio: pong` | TRACE | `sid` |
| socketio | `socketio: connection accepted` | DEBUG | `sid`, `remote_addr` |
| socketio | `socketio: event` | TRACE | `sid`, `nsp`, `event`, `ack_id`, `handler_found`, `duration`, `err` |
| socketio | `socketio: ack` | TRACE | `sid`, `nsp`, `ack_id`, `dir` |
| socketio | `socketio: emit` | TRACE | `sid`, `nsp`, `event`, `ack_id` |
| socketio | `socketio: broadcast` | TRACE | `nsp`, `room`, `event`, `recipients` |
| socketio | `socketio: handler error` | WARN | `sid`, `nsp`, `event`, `err` |

In v2, `socketio: handler error` (WARN) replaces v1's `socketio: unhandled error` for
handler errors. These v1 triggers keep `socketio: unhandled error`: decode errors, the
encoding error of a handler result, a CONNECT to an unknown namespace and an overflow. So
do two triggers new in v2: an unknown event without `OnRaw`, which v1 dropped
silently when the socket had no per-socket middleware (`Conn.Use`), and a `Data` value that cannot be encoded.
Two v1 triggers are not logged in v2: a middleware or `OnConnect` rejection, which v1
logged when no `OnError` was registered, and an emit's encoding error, which is returned
to its caller. Both records log WARN unless an
`OnError` handler receives the error (the namespace's on the server, 2.3S; the client's,
2.3C; the table in 2.3 *v1 continuity*, *Callbacks*), which then lowers them to at most
DEBUG. No `Hooks` field, chained or not
(including `LoggingHooks`, the OTel bridge and `contrib/admin`), counts as a registered
error handler.
On top of these it adds `rtt` on pong, `rooms`, `except` and `local` on broadcast, and
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
log keys, the full `request rejected` reason list, the hook contract (goroutine, non-blocking, no `Emit`, no panic recovery),
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
`TestNoBadKeyAttrs` in the logging tests is ported to the v2 scenario and passes at `trace` with the current message pattern and key list extended by the keys of the 2.4 records table and the keys added on top of it; `docs/OBSERVABILITY.md` owns the v2 list.
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
session close and the v1 features v2 does not carry: permessage-deflate,
`httpCompression`, the `io` cookie and the per-emit `compress` flag, which no v2 stage plans (`BroadcastFlags` is
`{Local bool}`); leaving those features out of v2 is a plan choice of this stage, not an owner
decision, and the owner has not been asked: 2.5 puts the question before it starts, and a
yes adds them to a v2 stage first, with the migration item rewritten), `docs/V2_PROTOCOL.md` update, `docs/OBSERVABILITY.md`,
`contrib/otel/README.md`, tags `v2.0.0` and `v2/contrib/otel/v2.0.0` from the same commit, only
after the owner's declaration and after `v1.5.0`, as specified in the
[release policy](../CONTRIBUTING.md#releases). Verify that the module path and every
`v2/_examples/*` module/import use `github.com/sshaplygin/go-socket.io/v2`, and that
the `v2/README.md` badge and links target v2 (the grep below and the Acceptance).
Update the links in `engineio/README.md` and the import paths of `contrib/otel` and
`adapters/*`. Task 2.5D migrates all non-Redis examples to
`socket.io-client@4` and the generic API, pinning maintained framework versions
compatible with Go 1.22; Redis examples remain deferred to 4b.

`docs/MIGRATION.md` owns the v1-to-v2 facts; 2.5M diagnostics and the v2 godoc link to
its anchors. Besides the two items above it covers:

- mapping tables from each v1 declaration consumers use to its v2 form, with the 2.5M
  category of each: server construction and options, Engine.IO options and transports,
  namespace callbacks, `Conn` to `*Socket`, rooms and broadcast, acknowledgements, socket
  data, the client;
- the wire upgrade: Engine.IO 3 to 4, Socket.IO 4 to 5, JavaScript `socket.io-client`
  1.7/2.x to 4.x and its API changes, the explicit CONNECT to `/`, per-namespace socket
  IDs; no tool can make this change for the JavaScript side;
- the acknowledgement choice: `PlainAck` keeps the v1 wire and `(value)` callbacks, the
  error-first default needs `(err, value)` callbacks in every client; with `AckTimeout` and
  the ordering of v1 ack callbacks against a blocking `EmitWithAck`;
- behaviour that compiles but changes, covering every difference from v1 that 2.1 E2b and
  2.3 *v1 continuity* state, at least: the CORS default (v1 allowed every origin with
  credentials unless `WithCORS(false)`; v2 writes CORS headers only when `CheckOrigin`
  allows, which is what v1 `WithCORS(false)` did, and `DisableCORS` also removes those,
  2.1 E2b), no `io` cookie, bounded per-socket queues whose overflow closes the socket,
  what `Server.Count` counts, the `handler_error` code becoming `internal_error`, the
  socket ID format and `Socket.Context()` as the lifecycle context; `OnError` no longer
  receiving middleware rejections or a nil socket, and receiving an `OnConnect` error after
  the CONNECT_ERROR is queued; an outbound queue overflow reaching `OnDisconnect` with its
  reason and no longer `OnError` with `ErrWriteBufferFull`; an unknown event, which v1
  dropped without a call, a record or a reply when the socket had no per-socket middleware
  (`Conn.Use`), now reaching `OnError` or a WARN record and,
  when it requests an ACK, answered with `unknown_event`; v1 `Server.Close` mapped to
  `Shutdown` (2.5M), which drains queued outbound messages until its context ends where
  v1 `Close` discarded them, and v2 `Server.Close` waiting for no `OnDisconnect` where v1
  `Close` waited up to 10 s (neither v1 nor v2 sends a namespace DISCONNECT when the
  server closes, so reconnecting clients reconnect after both); a middleware or
  `OnConnect` rejection no longer logged when no `OnError` is registered; an emit's
  encoding error returned to its caller, where v1 reported it to `OnError`, or logged it,
  and sent an ERROR packet;
  `OnConnect`, `OnDisconnect` and `OnError` registered once
  per namespace, a second call returning an error where v1 replaced the handler;
  `Handshake.Query` without the namespace CONNECT query v1 merged into it; a missing or
  extra event argument answered with `invalid_payload` where v1 zero-filled or ignored it
  (a `struct{}` payload excepted); the Go client's `Connect` returning after the CONNECT
  ack, with a CONNECT_ERROR as its error, and its `WithCloseTimeout` bounded by the ping
  timeout;
- the features not carried;
- worked examples as before/after excerpts: `_examples/gin-gonic` and
  `github.com/sshaplygin/socketio-live-lab`. The gin-gonic example shows the manual step
  after the tool for its `Serve` goroutine (2.5M, category B): the goroutine and its count
  in `remaining` are deleted, because nothing ends it when the HTTP server fails.

The PR that creates `docs/MIGRATION.md` adds its row to the documentation table of
`CLAUDE.md`.

DoD: both official suites (`engine.io-protocol/test-suite`,
`socket.io-protocol/test-suite`) pass in CI against the Go server; a CI Node script with
real `socket.io-client@4` covers connect, namespace with auth, ack both ways, binary,
disconnect, reconnect after a server restart through `Shutdown`; parser and payload unit tests cover every
example in the two specs; `go mod graph` shows no `gorilla/websocket`, and a lint rule
forbids direct `reflect` imports in production code outside `parser` (test reflection
is allowed); all stage-2 examples build and run against
`socket.io-client@4`; `go vet`, lint, `-race` green; idle-connection benchmark numbers
recorded; `docs/V2_PROTOCOL.md` lists every unimplemented item; the stage 2.4 DoD holds at
the commit to be tagged. Router integration: the `examples` CI job starts `_examples/default-http`,
`gin-gonic`, `go-echo`, `iris` and `gf`, and `TestFrameworkSmoke` in `_examples/smoke`
(own `go.mod`) completes a websocket handshake and an `add user` event answered by `login` through
each with the Go client; a test in `engineio` with a `ResponseWriter` that hides `http.Hijacker`
gets HTTP 501. Links: `pkg.go.dev/github.com/sshaplygin/go-socket.io/v2` renders the
module (the tagged version at tag time), and the grep below has no active v2 code/module imports of the old
path; historical v1 decisions, migration examples and changelog entries are allowed:

```sh
grep -rn 'googollee' --include='*.md' --include='go.mod' --include='*.go' v2 docs
```

Acceptance: a browser page on `socket.io-client@4` from CDN connects to
`_examples/default-http`, adds a user, receives `login`, and the server logs a
clean disconnect on an explicit client disconnect. `docs/MIGRATION.md` is enough to port
`_examples/gin-gonic` without reading library code. The 2.5M corpus test passes on the v1
`root:_examples/gin-gonic` and on the trimmed socketio-live-lab snapshot; that snapshot is the
M3 gate for the lab, and a port of the lab repository itself follows M3 and is not an M3
gate. A handler with a wrong payload type
fails at compile time. Every badge and link in `v2/README.md` resolves to the v2 module.
Pin Node, protocol-suite commits and client versions in CI. At tag time: test source
consumers against released root/contrib module versions without workspace `replace` directives,
publish the root tag before dependent module tags, and verify each module's minimum Go version. Redis examples and cluster acceptance are explicitly deferred to 4b.

**2.5M `socketio-migrate`.** A `golang.org/x/tools/go/analysis` multichecker with `-fix`
and `-diff` that rewrites a v1 project into v2 code. It is the nested module
`github.com/sshaplygin/go-socket.io/v2/cmd/socketio-migrate` with its own `go.mod`, so
`x/tools` stays out of the v2 module's `go.mod`; `x/tools` is pinned to a release whose
`go.mod` declares at most `go 1.22` (v0.30.0 does), keeping the Go 1.22 floor. Its `go.mod`
has no `replace` directive, because `go run` with a version refuses one. It imports neither
the v1 nor the v2 module: it matches v1 objects by import path in the type information of
the project, which must build on v1 when the tool runs. The v1 import paths are
`github.com/sshaplygin/go-socket.io` and every package below it outside `v2/`. A package
that imports `github.com/googollee/go-socket.io` or a package below it gets one category C
diagnostic linking the migration from the archived upstream to v1 ([V1.md](V1.md)) and no
rewrite, because the upstream API differs from v1. Users preview the edits with
`go run github.com/sshaplygin/go-socket.io/v2/cmd/socketio-migrate@v0.X.Y -fix -diff ./...`
(`-diff` alone prints no diff), apply them with the same command without `-diff`, then run
`go get github.com/sshaplygin/go-socket.io/v2` and `go mod tidy`. The version is the
module version `v0.X.Y` or `v1.X.Y`, since the module path has no `/vN` suffix; the git
tag that publishes it is `v2/cmd/socketio-migrate/v0.X.Y` (*Repository layout*), created
only on the owner's instruction. All analyzers apply in one `-fix` pass with edits that do
not overlap. A rewrite the tool cannot finish leaves an undefined identifier
`TODO_socketio_migrate_<reason>`, so the build fails exactly there. Every diagnostic links
a `docs/MIGRATION.md` anchor. Categories:

- A, mechanical fix: import paths; `socketio.Conn` to `*socketio.Socket`; `Room` and
  `SocketID` conversions; literal `With*` option lists to `Options` and `engineio.Options`
  literals (`WithMaxHTTPBufferSize` to `MaxPayload`, `WithWriteBufferSize` to
  `OutboundQueueGroups`); removal of `engineio.WithCookieEnabled(false)`, which is the v2
  behaviour, and of `engineio.WithCORS(false)`, whose behaviour the v2 default has whether
  or not a transport sets `CheckOrigin` (2.1 E2b); `Of` and namespace strings to
  `Server.Namespace(ctx, name)`, one variable per server and namespace string in a
  function declaration, declared in its body before the statement of the first use and
  reused by the function literals inside it (in the innermost block that encloses every
  use, when the server value is not in scope in the body); the namespace callbacks, socket accessors,
  `Count`, `RoomLen` and `To` of 2.3 *v1 continuity*; `Conn.Namespace()` to the `Name()` of
  the namespace on which the enclosing callback or handler is registered (category C where
  there is none); `Context` and `SetContext` to `Data` and `SetData`; the client
  constructor, options and callbacks; `Serve()` used as a statement or in a `go` statement,
  removed, since v2 has no `Serve` loop.
- B, fix with a diagnostic for review: untyped `OnEvent` to
  `NewAckEvent[T, R](name).PlainAck().Handle`, and a handler without a result to
  `NewEvent[T](name).PlainAck().Handle`, so a peer that requests an ACK gets `[]` as from v1
  (`Args2` for two arguments), with `return x` becoming `return x, nil`; a handler without a
  payload parameter to `T` = `struct{}` (2.3 *v1 continuity*, *Events without a payload*),
  whose handler ignores the value, and an emit without arguments to an emit of
  `struct{}{}`; a payload parameter of type `interface{}` to `json.RawMessage`; the
  client's `OnEvent` to `HandleClient` under the same rules; `Emit` to
  descriptor emits, where `T` is the static type of the single argument (`Args2` for
  two) and the descriptor is written inline at the call site, as
  `NewEvent[T](name).Emit(...)`; an argument of interface type gets
  `TODO_socketio_migrate_emit_interface` and an emit with more than two arguments
  `TODO_socketio_migrate_emit_args` in place of the descriptor; an emit with an ack
  callback, on a socket or the client, to
  `go func() { r, err := ev.EmitWithAck(ctx, to, v); if err != nil { return }; f(r) }()`,
  where `ev` is `NewAckEvent[T, R](name).PlainAck()` so the reply of a v1-style peer is
  read as the result, `R` comes from the parameters of the callback `f` (`struct{}` for
  none, `Args2` for two), and `f`, unchanged, runs only for a reply, as in v1, the error
  being discarded; room and
  namespace broadcasts to `EmitTo`; an adapter generated in the user package where a `Conn`
  flows into a user interface (below); the client's `Connect()` to `Connect(ctx)` with the
  `ctx` of the rule below, which now returns after the CONNECT ack; server
  `WithCloseTimeout` removed, because v2 bounds an application close by the session ping
  timeout and `Shutdown` by its context (2.0); a `Serve()` whose result is assigned or used
  to `func() error { <-ctx.Done(); return nil }()` with the innermost `context.Context`
  parameter of an enclosing function, and to `TODO_socketio_migrate_serve` without one, the
  diagnostic saying that the result now arrives when that context ends, not when the server
  is closed, so code that waits for it after `Shutdown` or `Close` blocks until the context
  ends and needs the manual change MIGRATION.md shows; v1 `Server.Close()` to `func() error { ctx, cancel :=
  context.WithTimeout(context.Background(), 10*time.Second); defer cancel(); return
  s.Shutdown(ctx) }()`, keeping v1's fixed 10 s wait for `OnDisconnect` (2.3 *v1
  continuity*, *Callbacks*), the diagnostic saying that `Shutdown` also drains queued
  outbound messages within those 10 s, which v1 `Close` discarded. A server on v1's permissive CORS default (no `WithCORS`
  option, or `WithCORS(true)`, which is removed) gets no inserted `CheckOrigin` and no
  `DisableCORS`. Its diagnostic, saying that cross-origin browsers lose access unless the
  application sets `CheckOrigin` and linking the MIGRATION.md CORS anchor, is emitted only
  when a configured `*polling.Transport` or `*websocket.Transport` (both, without
  `WithTransports`) has no `CheckOrigin` in a literal the tool reads, since v1's explicit
  transport `CheckOrigin` already overrode the permissive default. The tool never inserts
  `DisableCORS`; MIGRATION.md describes it for applications that write their own CORS
  headers in front of the server.
- C, diagnostic only: option lists built at run time; handlers with more than two
  arguments or an `interface{}` type switch; `OfFunc`, per-socket `Use`, `Volatile`,
  `Compress`, `WithCookie` and `WithCookieEnabled(true)`, HTTP compression and
  permessage-deflate options; the client's `OnPing`, `OnPong` and `Socket(nsp)` and the v1
  typed `RegisterEvent`, `SendEvent` and `RequestAck`; `EIO=3` literals; the JavaScript
  client upgrade, reported once per package that imports a v1 package, at the first v1
  import of its first file in file-name order. Every v1 declaration the categories above do not
  list is category C, with a diagnostic linking the MIGRATION.md mapping table and the
  code left as it is, for example `Conn.Request`, `Conn.EngineConn`, an `AckFunc` handler
  parameter, `Server.ForEach`, `Rooms`, `ClearRoom`, `Remove` and `Engine`, and the options
  `WithAdapter`, `WithHooks`, `WithUpgrades`, `WithSessionIDGenerator`, `WithRequestChecker`
  and `WithConnInitor`.

*Errors and contexts.* v2 adds error results that v1 did not have. For a construction or
registration call (`NewServer`, `Server.Namespace`, `Handle`,
`HandleClient`, and the namespace's `OnConnect`, `OnDisconnect` and `OnError`), the tool returns the error when the innermost enclosing
function has an error as its last result (`return` with the zero values of the other
results and `err`); otherwise it inserts `if err != nil { panic(err) }` with a category B
diagnostic saying that the call could not fail in v1 and that the application should
propagate the error. It never changes a function signature, because the callers may be in
other packages or modules. For a runtime operation whose v1 form returned no error
(`Join`, `Leave`, `LeaveAll`, a socket or client emit, `RoomLen`), the error is
discarded as v1 discarded it: `_ =` for a statement and `v, _ :=` where the value is used,
which for `RoomLen` is the partial result of 2.2. Where the v1 form returned
an error, the v2 error takes its place. A v2 target with one more result, as `EmitTo`
with its `BroadcastResult`, gets a blank for it: `_ = x` becomes `_, _ = x`, `err := x`
becomes `_, err := x`, and any other use of the error becomes
`func() error { _, err := x; return err }()`. Each inserted `ctx` is, in this order:
the `ctx` parameter of the v2 handler or callback the tool wrote around the call; the
innermost `context.Context` parameter of an enclosing function; `s.Context()` for an
operation on one socket `s`; `context.Background()`.

*Generated adapter.* Where a v1 `Conn` value flows into an interface that the project
declares (the lab's `Wire`), the tool generates an adapter type in a form that a
`SuggestedFix` can carry: one edit that appends the declaration to the end of an existing
file, so `-fix -diff` shows it and nothing is written without `-fix`. No file is created.
Each package with conversion sites gets one adapter per interface, in that package, also
when the interface is declared in another one. The file is the one holding the package's
first conversion site to that interface, by file name and then position, non-test files
before `_test.go` files, so a package and its test variant produce the same edit. The type
is unexported and named `socketioMigrate<Interface>Adapter` (`socketioMigrateWireAdapter`),
with the smallest suffix `2`, `3`, ... that avoids a name already in the package scope.
The imports it needs are added by the edit that rewrites that file's import declaration,
so the edits do not overlap. The adapter is a struct holding the `*socketio.Socket` and the
`*socketio.Namespace` on which the enclosing callback is registered; each conversion site
becomes a pointer to a composite literal of it (a site with no such namespace gets
`TODO_socketio_migrate_namespace`). Each interface method that matches a v1
`Conn` method gets a body: `Emit(event string, args ...interface{})` encodes each argument
as one positional argument with `parser.JSON[any]` at the default limits and joins them
with `parser.Concat`, so a `parser.BinaryValue` such as `socketio.Binary`, at any depth,
becomes an attachment as with a descriptor (a v1 `parser.Buffer` value falls under the
default rule of category C); it builds the packet with `parser.EventPacket` and the
namespace's `Name()` and sends it with `SendPacket` and `s.Context()`. An encoding or send
error drops the emit and is logged on `Namespace.Logger()` with the event name: a send
error at DEBUG, since v1's `Emit` dropped a packet on a closing connection without a
report, and an encoding error at WARN, since v1 reported it to `OnError`, or logged it at WARN,
and sent an ERROR packet. The adapter neither calls `OnError` nor sends an ERROR packet. A
func argument, which v1 took as an ack callback, fails the encoding and so drops the emit.
The diagnostic at the adapter names both changes.
`Join` and `Leave` convert each name to `Room`, and they and `LeaveAll` discard the error
as v1 did; `Close` returns `Socket.Close()`; the accessors map as in category A. A method
without a v2 target gets `TODO_socketio_migrate_conn_<method>`.

Staging: T1 (imports, options, `Conn`, IDs and the frozen room operations `Join`, `Leave`
and `Namespace.To`/`Except`) may start before 2.3S; a T1 golden uses only declarations
already on `master` when it merges, so the `MaxPayload` rule and its golden wait for 2.1
E2a. T2 (events, acks, the callbacks, the 2.3S accessors `RoomLen`, `Rooms`, `LeaveAll`,
`Socket.To`, `Count` and `Data`, the client, `Serve` and `Close`, the generated adapter)
starts once the 2.3S and 2.3C declarations it targets are on `master`; *Errors and
contexts* applies from the stage that rewrites each call; T3 (category C and the corpus) runs in 2F with 2.5D. DoD: `analysistest` golden
pairs for each analyzer, including the four CORS cases (no option without and with
`CheckOrigin` on both transports, `WithCORS(false)` with and without a transport
`CheckOrigin`), a lab-shaped option list, zero-argument handlers and emits, a handler
without a result under `PlainAck`, an `interface{}` payload, `Serve()` as a statement, in
a `go` statement and with a used result with and without an enclosing context, `Close`,
`Conn.Namespace()` with and without a namespace, a registration call in a function with an
error result and in one without, each `ctx` position, a namespace string used inside a
handler closure, an emit with an ack callback on a socket and on the client, a client
`OnEvent`, a broadcast `Emit` whose error is discarded, assigned and returned, the
generated adapter, as the golden of the file it is appended to, with and without a name
collision, and a `googollee` import; a harness
that builds every golden output against the v2 module of the same checkout in a temporary
module, so a drift between the 2.3S or 2.3C names and the tool fails: an output without
`TODO_socketio_migrate_` identifiers or category C diagnostics must build, and any other
must fail, and every error of its type check, run with `go/types` so that all errors are
collected and not only the first, lies on a line within the source range of such an
identifier or of the node a category C diagnostic reports; an error that follows from a
value left unconverted there (a later use of a variable declared in that range) is matched
by listing the line of that use in the golden, and an error on any other line fails;
from T3, when 2.5D has created
`docs/MIGRATION.md`, a test that every diagnostic anchor exists there (T1 and T2 name their
anchors in their golden files, and T3 adds the sections); a corpus test that runs the tool
on a copy of the v1 `root:_examples/gin-gonic` and on a trimmed snapshot of
`github.com/sshaplygin/socketio-live-lab` in `testdata` (its source commit recorded there),
both type-checked against the v1 module of the same checkout, applies `-fix` and builds the
result against the v2 module. The snapshot keeps, unchanged apart from removed unrelated
code, `internal/lab/server.go` (the `NewServer` call with its options, `OnConnect`,
`OnDisconnect`, `OnError`, the `OnEvent` loop and the `Count` admission check),
`internal/lab/hub.go` (the `Wire` interface and its calls), the types they use and
`cmd/bot/main.go` (`client.New` with its options, `OnEvent`, emits with ack callbacks, the
callbacks and `Connect`). The corpus outputs contain no `TODO_socketio_migrate_`
identifier and are compared with golden files; they contain the `panic(err)` checks of
*Errors and contexts* for the registration calls in gin-gonic's `registerChat`, the lab's
`NewServer` and the two `HandleClient` calls in the `main` of the lab's bot, one `nsp`
variable in `registerChat` that its handler closures reuse, gin-gonic's broadcast emits as
`_, _ =`, gin-gonic's `Serve` result rewritten with the `ctx` of `serveAfter` exactly as
category B states, without the manual change, its `Close` rewritten to `Shutdown`, the
bot's emits with ack callbacks on plain descriptors, and the lab's
`socketioMigrateWireAdapter` appended to `internal/lab/server.go`, which holds the
conversion site. The expected diagnostics are in a golden file, and the only category C
diagnostics are the JavaScript client upgrade, one per package, and `EIO=3` literals. The corpus test also runs the ported gin-gonic server: 1 s after
startup a polling handshake and a CONNECT to `/` succeed and the process is still running,
and after its context is cancelled it exits with status 0 within 15 s. A CI job of its own
runs the `analysistest` goldens and the build harness on Go 1.22, with `GOTOOLCHAIN=local`
so that no newer toolchain is selected, and on the stable Go. The corpus test runs on the
stable Go only, because its inputs do not build with Go 1.22: `_examples/gin-gonic`
declares `go 1.26.0` and requires `gin` v1.12.0, whose `go.mod` declares `go 1.25.0`, and
the lab declares `go 1.25.0`. It skips itself on a toolchain older than the highest `go`
directive of its modules, and the stable Go run fails when it is skipped. `CLAUDE.md` gets
the layout row of the module.

## Stage 3. Realtime chat example (`_examples/chat/`, own go.mod)

- **Server**: namespace `/chat`; `Auth[Credentials]` middleware reading the nickname
  from the auth payload; rooms; typed events `Message` (ack returns id and timestamp),
  `Typing`, `History` (ring buffer of 50 per room), `Presence` on join/leave; direct
  messages via the socket-id room; binary image attachment; graceful shutdown;
  `/metrics` through `contrib/otel` and `otel/exporters/prometheus`.
- **Upstream parity**: the upstream chat connects with `io()`, so the parity check runs
  against the default namespace `/` and coexists with the richer `/chat` namespace on
  the same server; the two share no state (the default namespace has its own user
  counter, no rooms, no history, no `Auth`). Pinned source: `socketio/socket.io`
  commit `1eaa582d3b453e3e6f522300ed05b10da0a0799b`, directory
  `examples/chat/public` (`index.html`, `main.js`, `style.css`), vendored in
  `_examples/chat/upstream/`.
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

DoD (upstream chat parity): the vendored upstream client is
byte-identical to the pinned commit except the one `socket.io-client` script tag, which
points to a pinned `socket.io-client` 4.x build, and it works against the Stage 3
server on the default namespace with the upstream names and payloads: client to server
`add user`, `new message`, `typing`, `stop typing`; server to client `login`
`{numUsers}`, `user joined` and `user left` `{username,numUsers}`, `new message`
`{username,message}`, `typing` and `stop typing` `{username}`; the sender is excluded
from every broadcast. A CI test in the `examples` job (two Go clients, or a Node
`socket.io-client` 4.x script) asserts this and diffs the vendored files against the
pinned commit.

Acceptance: owner runs the single-server compose stack, opens two browser tabs,
exchanges messages, sees typing and presence, uploads an image and verifies graceful
shutdown; the owner also opens the vendored upstream client on the default namespace
in two tabs and sees login count, join/leave, messages and typing as the upstream demo
shows them, and confirms the parity CI test is green (the parity DoD line). Define the load rate as per-client (5000 messages/s total), fix message
size, room/fan-out distribution and run duration, and record host/Go settings with
latency, errors, queue peaks and memory; do not use an unspecified workload as a gate.

## Stage 4b. Independent adapters: Redis and NATS

Each adapter has its own `go.mod` and CI job and is tagged independently
(`adapters/redis/v2.0.0`, `adapters/nats/v2.0.0`). The root `go.mod` has no Redis or
NATS dependency; it does carry `vmihailenco/msgpack/v5` (with its `tagparser/v2`), which
`adapter/codec` imports since 2.2. Both depend on the root module as a normal versioned dependency and on
`adapter/codec` for the message format. At tag time (M5 is accepted before it): release root `v2.3.0`, containing
`adaptertest` and any shared-codec additions, before tagging adapter modules, and verify
root and adapter consumers/tests against published versions without local replacements.
In this stage's tests (`adaptertest` and both backend suites) the adapter's request
timeout is 5 s. A *recovery poll* calls `Sockets` every 50 ms and passes at the first nil
error within 2 s (it fails at 2 s); every earlier call returns the local data and an
error, because restoring ends only when a flush or confirmation read completes, which a
single call cannot be required to hit.

- **`adapters/redis`**: compatibility with the non-sharded
  [`@socket.io/redis-adapter@8.3.0` wire format](https://github.com/socketio/socket.io-redis-adapter/blob/8.3.0/lib/index.ts)
  for stage 2.2 operations; cluster broadcast-with-ack is excluded. Channels:
  `<prefix>#<nsp>#`, `<prefix>#<nsp>#<room>#`, `<prefix>-request#<nsp>#`,
  `<prefix>-response#<nsp>#` and targeted `<prefix>-response#<nsp>#<uid>#`.
  Broadcast messages are msgpack `[uid, packet, opts]` via `vmihailenco/msgpack/v5`
  matching notepack output; supported request/response messages use Node's JSON
  encoding. Freeze fixtures for every supported operation, including
  `publishOnSpecificResponseChannel=true` and false (the Node corpus and its oracle are
  in `adapter/codec/testdata`, landed with 2.2). The injected
  `redis.UniversalClient` (the type `redis.NewUniversalClient` returns, which for one
  address holds a `*redis.Client`) must send every command to one Redis master, so only
  a `*redis.Client` (from `redis.NewClient` or `redis.NewFailoverClient`) is accepted;
  any other implementation, such as `*redis.ClusterClient` or `*redis.Ring`, is
  rejected at construction with the exported `ErrUnsupportedRedisClient`: counting
  peers on a Cluster needs PUBSUB NUMSUB summed over every master, as redis-adapter
  8.3.0 `lib/util.ts` does, and a Ring sends subscriptions and keyless PUBLISH and
  PUBSUB to different shards. A `*redis.Client` that routes to a replica
  (`FailoverOptions.ReplicaOnly`, or `NewClient` addressed to a replica) is
  unsupported, and the constructor godoc says so: a PUBLISH or PUBSUB NUMSUB on a
  replica reaches only that replica's subscribers, and go-redis v9 exposes no
  client-side way to detect it (the read-only flag of `redis.Options` is unexported;
  `ReplicaOnly` only selects the failover dialer; checked against v9.7.0, and the
  adapter's PR re-checks them against its pinned version). The adapter does not ask the
  server for its role: miniredis has no ROLE command. Bound request time and
  reconnect subscriptions with backoff; the backoff's initial and maximum delays are
  documented options.
  For 2.2 *Readiness*, construction and every resubscribe read the PSUBSCRIBE and
  SUBSCRIBE confirmations within the `SubscribeTimeout` option (default 10 s). Every read
  is bounded by the time left, and construction also stops when the factory context ends:
  a go-redis `ReceiveTimeout` reads without that context, so the adapter closes the
  attempt's `PubSub` from `context.AfterFunc`, stopped once the attempt is confirmed; when the stop reports that
  the function already started, construction fails with the context error instead of
  returning the adapter. An
  unconfirmed resubscribe is a failed attempt that closes its connection and grows the
  backoff. Each attempt uses a new `PubSub`, and the adapter closes a failed one as soon
  as it observes the error: after a connection error a go-redis `PubSub` redials and
  resends its subscriptions inside `Receive`, outside the backoff. The adapter becomes
  restoring before it logs `socketio: adapter subscriber lost`. The expected peers are
  PUBSUB NUMSUB of the request channel minus this instance, floored at 0; when NUMSUB
  fails they cannot be determined. Deterministic tests run on miniredis with a pre-hook,
  installed on the running server, that holds PSUBSCRIBE and SUBSCRIBE and releases the
  held commands all at once or one at a time, in the order they reached it (the v1 helper
  `delayRedisSubscriptions`, PR #18, holds PSUBSCRIBE). The *live values* are miniredis
  NUMPAT, NUMSUB of the request and response channels and `CurrentConnectionCount`. They
  are *settled* when they are equal on two reads 50 ms apart; each case first PINGs the
  injected client and waits for that, at most 5 s, and records them as the baseline.
  *Nothing left* means they are settled within 5 s of releasing the hold with NUMPAT and
  NUMSUB equal to the baseline and the connection count not above it, and the client still
  answers PING. A *peer* is another adapter with a socket, on its own client of the same
  miniredis; each case below says whether one exists, and the baseline is recorded after
  it is constructed.
  - 4R-T1: a peer exists. The test installs the hold, starts construction and, once the
    hook holds its first command, releases that one only. 200 ms later construction has
    not returned and the hook holds a second command (an adapter reading one confirmation
    returns earlier); releasing that one returns construction within 500 ms. The test adds
    a socket to the new adapter immediately after construction returns and only then
    issues a peer's broadcast, with no wait: the socket receives it within 1 s. The peer's
    `Sockets`, issued after the add, lists it.
  - 4R-T2: no peer. With `SubscribeTimeout` 200 ms and a 2 s hold, construction returns
    an error within 1 s, leaving nothing.
  - 4R-T3: no peer. Server `Close`, called after the hook has held its first command of a
    namespace creation, returns at once; with the hold still in place, the creating call
    returns at once after `Close` with an error matching `ErrNamespaceClosed`. The hold
    is released only then, leaving nothing.
  - 4R-T4: no peer. `Sockets` and `FetchSockets` return the local data and nil at once;
    with NUMSUB failing (pre-hook error), the local data and an error at once.
  - 4R-T5: no peer until the last step. The injected client's `Dialer` records every
    connection it dials. With a live adapter that has a socket and the baseline recorded, the test installs the hold first (only PSUBSCRIBE
    and SUBSCRIBE are held, and an established subscription sends neither until its
    connection closes, so no resubscribe reaches miniredis before it), then closes every
    recorded connection. Once the `subscriber lost` record is logged, `Sockets` returns
    the local sockets and an error at once. miniredis is not restarted: `Restart` builds
    a server without the pre-hook. With `SubscribeTimeout` 200 ms and the backoff's documented
    initial and maximum delay options set to 50 ms and 200 ms, the hold stays until it has held the first command
    (PSUBSCRIBE or SUBSCRIBE) of three distinct server-side connections within 10 s, or
    the case fails; an adapter attempt and the go-redis redial inside `Receive` are each
    a new connection. When the third has held its first command, `Sockets` returns the
    local sockets and an error at once, nothing being confirmed (an adapter that stops
    restoring when it creates or dials the new connection returns nil). The test then
    releases the held commands in arrival order up to the third connection's first one
    only; 50 ms later `Sockets` still returns the local sockets and an error (one of two
    confirmations), and the rest is released. Leaks are checked on the server only, after
    the release (a held connection stays counted until its command returns): once the live values are settled, a recovery poll passes, and
    nothing is left. A leaked attempt is an open
    connection miniredis counts whatever the client believes; the PR that adds the test
    also shows the check failing on a variant that leaves a failed attempt's connection
    open (a read with a timeout the library does not treat as fatal, and no `Close`). A
    peer, constructed only then, broadcasts, and the broadcast reaches the adapter's socket
    within 1 s.
  - 4R-T6: construction with a `*redis.ClusterClient` or a two-shard `*redis.Ring`
    returns an error matching `ErrUnsupportedRedisClient` (`errors.Is`).
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
  client. Heartbeats are sent every `HeartbeatInterval` (option, default 5 s, 200 ms in
  tests). For 2.2 *Readiness*, construction flushes after subscribing with a context
  derived by `context.WithTimeout` from the factory context and the documented
  `SubscribeTimeout` option (default 10 s; `FlushWithContext` needs a deadline). The
  flush confirms only the connected server (the limit stated in 2.2 *Readiness*); the
  adapter README says so and no case covers the rest. The adapter is restoring while
  `nc.IsConnected()` is false and from any reconnect until a flush succeeds after it. It
  sees a reconnect, even one shorter than a status poll, by comparing `nc.Stats().Reconnects`
  with the value read before its last successful flush; it polls every 100 ms and checks at each query,
  and never replaces the application's handlers. The expected peers are undetermined
  until one `HeartbeatInterval` after construction, so for that long every cluster query
  returns the local data and an error, also with no peer; the adapter README says so.
  Test with an embedded `nats-server/v2`, no Docker, and a test TCP proxy between client
  and server. The proxy listens on one fixed address, dials the server anew for each
  accepted client, closes the client side when the upstream closes, and has three controls.
  `Stall()` stops forwarding client bytes on live connections and buffers them;
  `StallNext()` does the same for the next connection that completes its handshake: the
  proxy engages the stall before it forwards the server's first PONG, so nothing the
  client sends after the handshake passes, and a connection whose upstream dial fails
  never completes one; `Release()` forwards the buffered bytes in order and ends the stalls. *Nothing
  left* is read after `Release()` and a following `nc.FlushTimeout(2 s)`, which returns
  once the server has processed everything the client sent before it: the server's
  subscription count and `nc.NumSubscriptions()` equal their values before construction.
  A leaked SUB reaches the server on release and raises the count; a SUB followed by its
  UNSUB leaves it unchanged. Every `Test4N_T<n>` builds its own server and repeats the
  steps it names.
  - 4N-T1: no peer. `HeartbeatInterval` plus 100 ms after construction the test shuts the
    embedded server down; `nc.IsConnected()` becomes false within 10 s (the test fails
    beyond it); `Sockets` and `FetchSockets` then return the local data and an error at
    once.
  - 4N-T2: with `Stall()` applied, construction with `SubscribeTimeout` 200 ms returns an
    error within 1 s; with `SubscribeTimeout` 10 s and the factory context cancelled
    after the proxy buffered the SUB, it returns an error at once. `Release()` comes only
    after the error, then each leaves nothing. The PR that adds the case shows it failing
    on a variant that skips the unsubscribe.
  - 4N-T3: `HeartbeatInterval` 2 s, a peer adapter running. `Sockets` issued as soon as
    construction returns returns the local sockets and an error at once; 2.1 s after
    construction it returns the peer's sockets and nil within 1 s.
  - 4N-T4: no peer; the connection has `MaxReconnects` -1 and `ReconnectWait` 100 ms.
    The test calls `StallNext()` right after construction returns, then does the 4N-T1 shutdown with
    its assertions, restarts the server on the same address and waits for
    `nc.IsConnected()` to turn true within 10 s. The resent subscriptions and the
    adapter's flush then sit in the proxy, so `Sockets` returns the local sockets and an
    error at once although `nc.IsConnected()` is true. After `Release()` a recovery
    poll passes.
  - 4N-T5: the application's `DisconnectedErrHandler` and `ReconnectHandler` count
    their calls; the test repeats the 4N-T1 and 4N-T4 steps and then closes the adapter;
    the first handler was called once and the second once.
- Both adapters report through `Namespace.Hooks()` (paired `AdapterPublishStart/End`
  and `AdapterReceiveStart/End`) and use `Namespace.Logger()` for other library-owned diagnostics.
  The Redis adapter logs WARN `socketio: adapter publish failed` on a PUBLISH error,
  `socketio: adapter bad message` on a message it cannot decode, and
  `socketio: adapter subscriber lost` once per lost subscription before the backoff
  reconnect, with `nsp`, `channel` and `err`; `channel` joins the
  `docs/OBSERVABILITY.md` key list and `TestRedisAdapterLogs` checks the three records;
  add isolation tests proving that two adapters attached to differently configured
  servers do not send logs to each other's handlers or the global fallback.
- **`adaptertest`** package in the root module: conformance suite any adapter runs
  against itself (like `fstest.TestFS`): join/leave, broadcast to room, except, local
  flag, fetch across two adapters, server-side emit, peer loss with timeout, both
  adapter hooks firing, and the generic 2.2 *Readiness* cases: for a backend with
  peers, a socket added to the new adapter immediately after construction returns, then
  a peer's broadcast issued with no wait, gets a nil error with `Published` true and
  reaches that socket within 1 s; a query
  with no peer, issued after the harness's `DiscoveryDelay` (0 for Redis, the
  `HeartbeatInterval` plus 100 ms for NATS), returns the local data and nil at once;
  with a socket added and the factory context cancelled after construction returns, and
  200 ms waited for every backend whatever its `DiscoveryDelay` (longer than any
  in-process cancellation), a peer's broadcast still reaches the socket and, after
  `DiscoveryDelay`, a query with a peer still answers until `Close`. The harness calls `AdapterFactory` itself with a context
  it cancels once the call returns, because `Server` cancels that context only in
  `Shutdown` or `Close` (2.3S R9 covers that path). They run as the subtests `Readiness/PeerBroadcast`,
  `Readiness/NoPeerQuery` and `Readiness/CtxAfterReturn` of `adaptertest.Run`, which
  each backend module calls from its wrapper test `TestAdapterConformance`. Held
  subscriptions and subscriber loss need a backend harness and stay in the backend
  suites (4R, 4N).
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
suite (embedded server, two servers) pass under `-race`; every 4R and 4N case
passes under `-race` as the test `Test4R_T<n>` or `Test4N_T<n>` (the 4R cases on
miniredis), and so does each `adaptertest` `Readiness/` subtest of the Redis and NATS
suites. In each adapter module `go test -race -count=1 -json -run
'^(Test4R_T[1-6]|TestAdapterConformance)$' ./...` (`Test4N_T[1-5]` in `adapters/nats`)
must report a pass for each of its ids and no skip, fail or missing id. The ids are
the test names and `TestAdapterConformance/Readiness/PeerBroadcast`, `.../NoPeerQuery`
and `.../CtxAfterReturn`: a skipped subtest does not fail its parent, so a CI script
(added with 4b) reads the `-json` events for every id and fails otherwise, and a PR
that adds a case adds its id there;
cross-language CI test:
one Go server and one Node `socket.io@4` server with `@socket.io/redis-adapter` share
Redis, a room broadcast from each side reaches a client on the other, and
`fetchSockets` from Node lists the Go socket. Msgpack fixtures captured from notepack cross-decode in
both directions with equal semantics; require exact bytes only for canonical
fixtures where map ordering and numeric representation are fixed.

Acceptance: the chat cluster runs with one Go and one Node instance behind the same
nginx on Redis, and with two Go instances on NATS, and both tabs see each other's
messages in each configuration.
The Go/Go profile additionally verifies that terminating node A leaves node B's
existing sessions alive and observable. Connection recovery to the failed node,
replay of missed messages and shared history remain outside this acceptance.

## Stage 5. Socket.IO Admin UI (required final product stage)

Entry: M3 for 5L, M5 for 5C (see *Split* below). Output: module
`github.com/sshaplygin/go-socket.io/v2/contrib/admin/v2` with its own CI/release
and direct connection from the unchanged official UI. Support hosted and
self-hosted static UI. Pin an upstream UI release and matching instrumentation
commit in 5L-A; record them in the module README. Reference contracts:
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
   additions compatible with the v2 API of Stage 2. Hook additions also update
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
   No global atomic cluster snapshot is promised. Add an optional adapter interface,
   separate from the command capability of item 4, to carry node
   snapshots/watermarks; ordinary `FetchSockets` alone cannot supply this
   synchronization boundary. Overflow or peer loss during initial
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
   Freeze result/error semantics in 5L-A: success means local application plus broker
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
   on each server. Release versions are listed in [Milestones](#milestones).

### Split: 5L single server, 5C cluster

The items above are delivered in two parts with separate gates.

**5L** (entry M3, milestone M3A) runs one Go server with the memory adapter and
needs neither Stage 3 nor Stage 4b. It implements item 1 except the adapter
snapshot capability, cross-node watermarks, peer-loss abort of initial sync, mixed
Node observation and node failure detection; the local snapshot and its watermark
use the same synchronization boundary that 5C later exposes through the adapter.
It implements item 2 with an in-memory session store behind the frozen
session-store interface; item 3 in full; item 4 for local sockets, with commands
acting through the memory adapter's implementation of the optional command
capability; item 5 in full. For item 6, 5L writes `docs/ADMIN_UI.md` (single-server
behaviour), `contrib/admin/README.md`, the `CLAUDE.md` rows and the README link, and
adds `_examples/admin` (own module, built and vetted by the v2 `examples` CI job of
2.5, whose target 5L extends to include it): one server with the memory
adapter, a minimal page on `socket.io-client@4` with emit, join-room and leave-room
controls, and a compose service with the pinned static UI.

5L-A freezes, so that 5C is additive after `contrib/admin/v2.0.0` and root
`v2.1.0` are tagged:

- the optional command capability (`SocketsJoin`, `SocketsLeave`,
  `DisconnectSockets`): signatures with `context.Context`, deadline handling and
  the result/error semantics of item 4 (success means local application plus
  broker acceptance for remote commands);
- the session-store interface: context-aware, error-returning, with session expiry,
  so a shared store can implement it;
- the root snapshot and watermark types: comparable and monotonic per server
  instance; the admin module maps them to `serverId`;
- the exact `supportedFeatures` list advertised with the memory adapter for each
  mode and the read-only setting, taken from the pinned instrumentation.

**5C** (entry M5 and M3A, milestone M6) adds the rest: node snapshots/watermarks as
a separate optional adapter interface discovered by type assertion (the 5L command
capability does not change), its Redis and NATS implementations, remote
`SocketsJoin`/`SocketsLeave`/`DisconnectSockets` with Node's unacknowledged bulk
command encoding on the broker, peer-loss abort of initial sync, a shared session
store, cluster-wide statistics and live updates without duplicates, the mixed
Go/Node fallback, node failure detection, the cluster section of
`docs/ADMIN_UI.md` and admin instrumentation in the chat compose stack.

DoD for 5L: event fixtures from the pinned upstream instrumentation match the Go
output, including positional arguments and date encoding. Browser tests against the
actual pinned UI cover authentication, reconnect/session reuse, statistics, socket
and room lists, transport upgrades, binary event display, live updates and
administrative commands on local sockets. Tests prove that read-only mode rejects
commands, payload redaction holds, admin events do not recurse, slow admin consumers
do not stall application sockets, and shutdown leaks no library-owned goroutines.
During initial sync and reconnect, continuously connect/disconnect sockets and
change rooms on the server; assert that the UI converges without resurrected
sockets, missed removals or duplicate entries. Exercise snapshot queue overflow and
actual UI reconnect. The memory adapter's 2.2 conformance tests gain local
`SocketsJoin`/`SocketsLeave`/`DisconnectSockets` cases and pass under `-race`; 4A
moves them into `adaptertest`. Benchmark results for disabled instrumentation,
production and detailed mode and the tested upstream versions are recorded in the
M3A release notes; M3 protocol and observability checks remain green.

Acceptance for 5L: owner starts the `_examples/admin` compose stack and opens the
unchanged Admin UI, both the pinned self-hosted service and the hosted UI. Two tabs
of the example page appear as live connections; sending events, joining and leaving
rooms and closing a tab update the corresponding views. The default read-only mode
rejects commands; with administrative commands enabled, a socket can join/leave a
room and be disconnected. Restarting the server is visible in the UI, and
reconnecting the UI refreshes its state.

DoD for 5C: the 5L DoD remains green on each node. Additionally, during initial sync
and reconnect, continuously connect/disconnect sockets and change rooms on two
nodes; assert that the UI converges without resurrected sockets, missed removals or
duplicate entries. Exercise the documented mixed-Node fallback after quiescence.
Race tests and the expanded `adaptertest` pass for all three adapters. Cluster tests
cover two Go nodes with Redis and NATS and a mixed Go/Node Redis cluster with
instrumentation on both nodes: an admin client connected to one node sees remote
sockets and server statistics and can administer remote sockets. Benchmark results
and tested upstream versions are recorded in the M6 release notes; earlier protocol
and observability checks remain green.

Acceptance for 5C: owner starts the chat compose stack and opens the unchanged Admin UI.
The UI shows both servers and their live connections; sending messages, joining and
leaving rooms and closing a tab update the corresponding views. With administrative
commands enabled, a remote socket can join/leave a room and be disconnected. Killing
one server is visible in the UI while the surviving server remains observable;
reconnecting the UI refreshes its state. Repeat with Redis and NATS. Passing this
acceptance completes product scope and opens the final benchmark stage.

## Stage 6. Final comparative benchmarks

Entry: M6, including Admin UI and the adapter updates. Earlier microbenchmarks
remain regression gates; this stage compares complete servers after product work.
Owner freezes the matrix in 6A before measurements; runner implementation can then
proceed independently in 6B. Reserved measurement hosts run one candidate at a time.

**Candidates and compatibility.** Compare our final v2, `zishang520/socket.io` and
the official `socket.io` JS/TS server running on Node.js. Pin exact commits/tags,
Go/Node versions, dependency lockfiles and build flags. Also record this fork's
v1 line (the repository-root module `github.com/sshaplygin/go-socket.io` at a commit pinned in 6A; `v1.5.0` once tagged) as a legacy baseline for shared scenarios, with a compatible EIO3 client;
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
new revision IDs; release them separately. M7 closes the benchmark scope.


## Milestones

| Milestone | Content | Release |
| --- | --- | --- |
| M2 | G2, Engine.IO v4, memory adapter and Socket.IO codec integrated; consumer experiments absorbed into their packages | Reviewed `v2-next` snapshot |
| M3 | Socket.IO runtime, client, observability, conformance, migration guide and tool | `v2.0.0`, `v2/contrib/otel/v2.0.0`; a `v2/cmd/socketio-migrate` tag (2.5M) |
| M3A | Admin UI on a single server (5L) | `v2.1.0` with the additive core changes, `contrib/admin/v2.0.0` |
| M4 | Single-server chat | `v2.2.0` |
| M5 | Broker adapters and cluster chat, including mixed Go/Node Redis | `v2.3.0`, then Redis/NATS module tags |
| M6 | Cluster administration (5C) | `v2.4.0` (`adaptertest` and capability additions), then Admin module and adapter minor releases |
| M7 | Comparative benchmarks and reproducible artifacts | Report revision |

Each milestone takes the next root minor at its tag; the numbers above assume the
order M3A, M4, M5, M6. Acceptance checks run before tagging. Publication checks that require released
module versions run at tag time. Tags require the owner's explicit instruction;
see [CONTRIBUTING.md](../CONTRIBUTING.md#releases). A reviewed M2 snapshot does not
receive development PRs; topic branches continue to target `master`.

## Out of scope

Engine.IO v3 in v2; connection state recovery; WebTransport; permessage-deflate
in v2; Redis sharded Pub/Sub, Cluster, Ring and replica-routed clients;
cluster broadcast ACKs; NATS JetStream persistence; framework-specific integration
packages; trace-context propagation through the Redis adapter.
