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
| 2C | 2B | 2.3S server/namespace runtime (root socket files, including the body of `Namespace.LocalSockets`); 2.3C client (`client/`) | typed Go/Node tests and lifecycle tests (including `TestNamespaceReadiness`, 2.3S) pass; dispatch baseline recorded |
| 2D | 2C | one owner propagates instance loggers across runtime packages | logger precedence/isolation tests pass |
| 2E | 2D | 2.4E Engine.IO hook fire points; 2.4S Socket.IO hook fire points; 2.4O OTel bridge (`contrib/otel`) against frozen hook fixtures | all hook, span, metric and overhead checks pass |
| 2F | 2E | 2.5T conformance/framework tests; 2.5D migration/examples/docs | M3 acceptance; publication verification at tag time |
| 3A | M3 | freeze chat event schema; then server, browser/CLI and load client in separate directories | M4 single-server acceptance |
| 4A | M4 | freeze codec fixtures and adaptertest cases; then Redis and NATS modules independently | each passes shared conformance suite |
| 4B | 4A | cluster chat profile; mixed Go/Node Redis tests in separate test directories | M5 cluster acceptance |
| 5A | M5 | freeze admin snapshot/capability and command-result contracts, and pinned UI fixtures | admin contract gate |
| 5B | 5A | core observers/snapshots; admin wire/auth module; Redis/NATS capability extensions; browser tests against fixtures | integrate and run M6 acceptance |
| 6A | M6 | benchmark owner freezes versions, workload matrix, resource budgets and result schema | comparison contract and correctness checks pass |
| 6B | 6A | our-v2, existing-Go and official-Node runners in separate directories; shared load generator owned by integrator | runners produce equivalent traffic/results |
| 6C | 6B | measurements sequentially on reserved hosts; analysis/report follows complete raw results | M7 reproducibility and report acceptance |

Each join builds/tests the whole v2 module and affected child modules. Mocks
support isolated development; integration and release gates exercise real peers.

## Stage 2. Socket.IO protocol v5 over Engine.IO protocol v4 (tag `v2.0.0`)

Protocol deltas are listed in [V2_PROTOCOL.md](V2_PROTOCOL.md#planned-engineio-v4-and-socketio-v5).
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
experiment and its `Unreleased` CHANGELOG entry (today `adapter-rooms`; `adapter-wire` went
with the first 2.2 PR, which created `adapter/codec`, `sio5-codec` was absorbed into `parser/` by 2.3P, and
`eio4-websocket`, `ws-bench` and the `internal/eio4` entry went with the 2.1 WebSocket PR). M2 acceptance checks the deletions. A draft PR does not satisfy a gate.

| Component (source PR) | Purpose | Consumer | State in `master` and retirement |
| --- | --- | --- | --- |
| `_experiments/adapter-rooms` (#9, landed as #45) | Node memory-adapter room corpus | 2.2 conformance | landed, unreleased; absorbed into the 2.2 tests by the PR that adds them, which deletes it |

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
| Lifecycle context of `Socket` and `Namespace`, disconnect, connection callbacks, `Socket.Data`, descriptor codec binding | moved out | they need the runtime design; each is an added method | 2.3S |
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
| `engineio/payload` (moved from `internal/eio4`) | polling codec, bounded `DecodeReader`, exact-wire `EncodeBatch`, fixtures/fuzz tests and pinned Node oracles | integrated by the polling transport (PR B): POST limits and status mapping, client batching, cancellation and deadlines, pause/upgrade lifecycle. Open: the open packet does not advertise `maxPayload` and the `EIO=4` check is absent until the session and server work (PRs D1/D2) |
| `engineio/transport/websocket` (moved from `internal/eio4` and `_experiments/eio4-websocket`) | complete-message EIO4 codec with binary/text fixtures and parser oracle; gobwas framing (masking, fragments, UTF-8, control frames, message limits, close statuses) with a pinned `ws` peer oracle | integrated by the WebSocket transport (PR C): `FrameReader`/`FrameWriter` over the codec, `ws.Dialer`, deadlines and buffer options kept. Open: EIO handshake, heartbeat and upgrade lifecycle (PRs D1/D2); the `EIO=4` check; limits and timeouts as options (PR E) |

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
  `maxPayload` and noop on upgrade. Backlog, owned by the 2.1 owner: the session pauses
  polling at `3probe`; move the pause to `5`, as the engine.io server
  pauses only on `5`, and in the same change rewrites the v2 upgrade note of
  `docs/V2_PROTOCOL.md` (*Implemented: Engine.IO protocol v3*, "If the client never sends
  `UPGRADE`, the paused polling connection is resumed", `:68-69` at `2667c25`), which
  stops being true then. `engineio/client`: the client uses `pingInterval+pingTimeout` to detect a missing server ping, and
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
  fragments together, until the limits options of PR E; the connection's `ServeHTTP` returns at
  once, because the upgrade hijacked the connection. Both `engineio.Server` and
  `socketio.Server` assert `var _ http.Handler`; a wrapped `ResponseWriter` without
  `http.Hijacker` is answered with HTTP 501 and an `engineio: request rejected` line with
  `reason="no hijacker"`, never a panic.
- CI job runs `socketio/engine.io-protocol/test-suite` (Node) against the Go server.
- `BenchmarkIdleConnections` (10k websocket connections, RSS and goroutines) recorded
  in `CHANGELOG.md` before and after the swap (both recorded there, with the commands, from
  `engineio/idle_bench_test.go`).
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
  was requested, otherwise only the error handler, if 2.0 defines one, or the log;
  malformed protocol envelopes
  close with `parse error`. For valid known events, server and client use this table:

  | Descriptor | Incoming ACK ID | Handler and response |
  | --- | --- | --- |
  | `Event[T]` | absent | run handler; errors go to the error handler, if 2.0 defines one, or the log |
  | `Event[T]` | present | run handler; reply `[null]` on success or the typed error envelope |
  | `AckEvent[T, R]` | absent | run handler, discard result; errors go to the error handler, if 2.0 defines one, or the log |
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
  semantics in the 2.0 API fixture; zero selects the bounded default, not unlimited. The
  option names are `OutboundQueueGroups`, `OutboundQueueBytes`, `HandlerQueueEvents`,
  `HandlerQueueBytes`, `MaxPendingAcks`, `MaxAttachments`, `MaxEventBytes`,
  `AttachmentTimeout`, `MaxConcurrentConnects` and `ConnectTimeout`, with `AckTimeout`
  above; G2 froze them.
- `BenchmarkEventDispatch` (root) is added with the new model, so stage 2.4 has a real
  baseline.
- Rewrite `Client` on the same generic API with websocket over `gobwas/ws`. This item delivers the v2 client; the v1 client is an independent implementation.
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
handler errors. Every other v1 trigger (decode errors, a marshal error, a CONNECT to an
unknown namespace, an overflow) keeps `socketio: unhandled error`. v2 has no `OnError`;
both records log WARN unless the API frozen in 2.0 adds an error handler, which then
lowers them to at most DEBUG when registered. No `Hooks` field, chained or not
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

DoD: both official suites (`engine.io-protocol/test-suite`,
`socket.io-protocol/test-suite`) pass in CI against the Go server; a CI Node script with
real `socket.io-client@4` covers connect, namespace with auth, ack both ways, binary,
disconnect, reconnect after server restart; parser and payload unit tests cover every
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
`_examples/gin-gonic` without reading library code. A handler with a wrong payload type
fails at compile time. Every badge and link in `v2/README.md` resolves to the v2 module.
Pin Node, protocol-suite commits and client versions in CI. At tag time: test source
consumers against released root/contrib module versions without workspace `replace` directives,
publish the root tag before dependent module tags, and verify each module's minimum Go version. Redis examples and cluster acceptance are explicitly deferred to 4b.

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
`adapter/codec` for the message format. At tag time (M5 is accepted before it): release root `v2.2.0`, containing
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

Entry: M5. Output: module `github.com/sshaplygin/go-socket.io/v2/contrib/admin/v2`
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
   on each server. At tag time, release additive core changes as `v2.3.0`, the module as
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
| M3 | Socket.IO runtime, client, observability and conformance | `v2.0.0`, `v2/contrib/otel/v2.0.0` |
| M4 | Single-server chat | `v2.1.0` |
| M5 | Broker adapters and cluster chat, including mixed Go/Node Redis | `v2.2.0`, then Redis/NATS module tags |
| M6 | Admin UI and cluster administration | `v2.3.0`, Admin module tag and adapter minor releases |
| M7 | Comparative benchmarks and reproducible artifacts | Report revision |

Acceptance checks run before tagging. Publication checks that require released
module versions run at tag time. Tags require the owner's explicit instruction;
see [CONTRIBUTING.md](../CONTRIBUTING.md#releases). A reviewed M2 snapshot does not
receive development PRs; topic branches continue to target `master`.

## Out of scope

Engine.IO v3 in v2; connection state recovery; WebTransport; permessage-deflate
in v2; Redis sharded Pub/Sub, Cluster, Ring and replica-routed clients;
cluster broadcast ACKs; NATS JetStream persistence; framework-specific integration
packages; trace-context propagation through the Redis adapter.
