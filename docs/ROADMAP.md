# Roadmap

Status: approved 2026-09-28, amended 2026-09-29 (observability, package layout, links,
framework integration). Owner: Sam Shaplygin.

This file owns the plan: stages, Definition of Done (DoD), acceptance criteria and
milestones. Released work moves to [CHANGELOG.md](../CHANGELOG.md). Current protocol
support lives in [PROTOCOL.md](PROTOCOL.md). How to build and test lives in
[CLAUDE.md](../CLAUDE.md).

## Starting point

The fork is identical to the archived upstream `googollee/go-socket.io` (last upstream
commit 2024-09-29). It implements Socket.IO protocol v4 over Engine.IO protocol v3 only,
so `socket.io-client` 3.x and 4.x cannot connect. The toolchain is stale (`go 1.16`,
`x/exp/slog`, golangci-lint v1 config, GitHub Actions v3/v4), the websocket transport
wraps `gorilla/websocket`, event handlers are dispatched through reflection, and the
Redis adapter has data races.

Observability is limited to one global `slog` logger used almost only at error level,
with no session or namespace attributes, no runtime level control, no metrics and no
tracing. The root package is a flat list of files whose name prefixes
(`namespace_*`, `connection_*`, `redis_*`, `session_*`) stand in for package boundaries.

## Decisions

| Question | Decision |
| --- | --- |
| Module path | stage 1 keeps `github.com/googollee/go-socket.io`, tag `v1.5.0`; stage 2 switches to `github.com/sshaplygin/go-socket.io/v2` |
| EIO=3 / socket.io-client 2.x in v2 | not supported; branch `v1` stays for old clients |
| WebSocket library | `github.com/gobwas/ws` (+ `wsutil`) replaces `gorilla/websocket` in stage 2.1, server and client side |
| Event API | generics-first, no reflection in v2; untyped escape hatch takes `json.RawMessage` |
| Redis client | `github.com/redis/go-redis/v9` |
| NATS client | `github.com/nats-io/nats.go`, core pub/sub and request-reply, no JetStream |
| Wire compatibility with Node `@socket.io/redis-adapter` | required for Redis: same channels and msgpack (notepack) encoding. NATS has no official Node adapter, so it reuses the same message encoding without a compatibility claim |
| Minimum Go | 1.22, `log/slog` from stdlib |
| Documentation language | English everywhere, including code comments |
| Log level at runtime | package `logger` reads `SOCKETIO_LOG_LEVEL` (`error`, `warn`, `info`, `debug`, `trace`) once at init into the exported `logger.Level` (`slog.LevelVar`). Every logger the library uses is wrapped by `logger.Wrap`: when the variable is set, `logger.Level` decides what is enabled; when it is unset, the application's handler decides. `engineio.Options.Logger` chooses the sink, never the level. Applications may call `logger.Level.Set` at runtime |
| Tracing and metrics API | hook structs `engineio.Hooks` and `socketio.Hooks` (nil-able funcs in the `net/http/httptrace` style, combined with `ChainHooks`) in the root module with no external dependency. `LoggingHooks` is the built-in implementation that produces the debug and trace log lines. The OpenTelemetry bridge is the separate module `contrib/otel`; the root `go.mod` has no `go.opentelemetry.io` dependency |
| Span policy | no per-connection spans. Spans exist for handshake, upgrade, namespace connect, inbound event, emit-with-ack, broadcast and adapter publish/receive. Session lifetime is covered by metrics and logs. Event spans link to the handshake span context stored on the socket context |
| Metric cardinality | `sid`, `socket_id`, `ack_id`, `remote_addr` and room names are never metric attributes. `event` is used only for registered events, otherwise `_unknown`. `nsp` is bounded to registered names or patterns. `reason` and `result` are closed enums; disconnect reasons use the Node strings |
| socketio-level options | v2 adds `socketio.Options{Engine *engineio.Options; Logger *slog.Logger; Hooks *Hooks; WriteBufferSize int; AckTimeout time.Duration}`; `engineio.Options` keeps engine settings only |
| Package layout | a group of files sharing a `<prefix>_` name prefix becomes its own package when it has no type cycle with the rest of its package; otherwise files are named by role and a prefix appears at most twice per directory. The target tree is in stage 1b |
| Router integration | `socketio.Server` and `engineio.Server` are plain `http.Handler`s, and that is the only integration surface; there are no framework-specific packages. The websocket transport needs `http.Hijacker` on the `ResponseWriter` (HTTP/1.1 only); this is documented in the `ServeHTTP` godoc and the README compatibility table |

## Stage order

1 → 1b → 2 → 3 → 4. Stage 1b (package layout) is a prerequisite to stage 2 and is the
first work on `master` after the `v1` branch is cut from `v1.5.0`. The adapter interface
(4a) is fixed at the start of stage 2 because the namespace rewrite for protocol v5
touches the broadcast layer anyway. Moving Redis and NATS into their own modules (4b)
follows stage 3.

## Stage 0. Documentation baseline

- English `docs/ROADMAP.md` (this file), project `CLAUDE.md`, `docs/PROTOCOL.md`
  absorbing the former `upgrade workflow.md`, trimmed `README.md`, `CHANGELOG.md`.
- Documentation ownership map is in [CLAUDE.md](../CLAUDE.md#documentation).

DoD: all Markdown is English (`grep -rlP '[\x{0400}-\x{04FF}]' --include='*.md' .` is
empty); each topic appears in exactly one file per the ownership map; `CLAUDE.md`
lets a new contributor run `make lint test` without reading anything else.

Acceptance: owner reads `CLAUDE.md` and `README.md` and confirms nothing is duplicated
and nothing needed is missing.

## Stage 1. Infrastructure and known bugs (tag `v1.5.0`)

No protocol or public API changes except `engineio.Options.Logger` and the additions to
package `logger` (`Level`, `LevelTrace`, `Wrap`). `gorilla/websocket` stays in v1; the
transport swap happens in stage 2 where the transport is rewritten.

1. **Toolchain and CI** (1 PR). `go.mod` → `go 1.22`; drop `golang.org/x/exp`,
   `io/ioutil`, `gofrs/uuid+incompatible` → `google/uuid`; bump `gorilla/websocket`
   to 1.5.3 and `testify`. `.golangci.yml` → v2 format. `ci.yaml`: `checkout@v4`,
   `setup-go@v5`, `golangci-lint-action@v8`, a job building every `_examples/*`,
   `govulncheck`. Makefile targets: `lint test test-race cover bench examples`.
2. **Logger** (3 PRs). Goal: an operator sets `SOCKETIO_LOG_LEVEL=debug` on a running
   deployment and follows one client through handshake, namespace connect, events and
   disconnect by `sid`, with no rebuild. The first PR (`engineio.Options.Logger`,
   `log.Println` removed from the connection code, constant messages in the session and
   socket.io code) and 2a have landed; see [CHANGELOG.md](../CHANGELOG.md). 2a differs
   from the text below in four points: the `nsp` attribute on namespace connections and
   the deprecation of `logger.Error`/`logger.Info` moved to 2b; `TRACE` rendering is
   opt-in through `logger.ReplaceAttr`, since a library cannot rename levels in an
   application's handler; `logger.Log` resolves `slog.Default()` at log time rather than
   at init, and `Level` starts at `LevelUnset` so `Level.Set` works without the variable.
   Remaining:
   - **2a. Levels, environment variable, sink.** `logger/logger.go` exports `Level`
     (`*slog.LevelVar`), read once at `init` from `SOCKETIO_LOG_LEVEL` (case-insensitive;
     unset means the application's handler decides; an invalid value logs one `WARN`
     and behaves as unset). `LevelTrace = slog.LevelDebug - 4`, rendered as `TRACE`.
     `logger.Wrap(l *slog.Logger) *slog.Logger` wraps the handler: when the variable is
     set, `Enabled` is `level >= Level.Level()` and `Handle` forwards unconditionally, so
     the application's handler level cannot hide library lines; when it is unset,
     `Wrap` delegates to the wrapped handler. `logger.Log` becomes
     `Wrap(slog.Default())`. `engineio.Options.getLogger()` returns
     `Wrap(opts.Logger)` or `logger.Log` instead of the bare `slog.Default()`. `logger.Error` and `logger.Info` become
     nil-safe and deprecated; new code takes a `*slog.Logger`. `socketio.NewServer`
     keeps the same logger and derives `With("sid", ...)` in `serveConn` and
     `With("nsp", ...)` in `newNamespaceConn`; `session.New` derives
     `With("sid", ..., "transport", ...)`. Packages without access to `Options`
     (`parser`, `engineio/packet`, transports, `engineio.Dialer`, `socketio.Client`)
     keep `logger.Log`, so the environment variable covers them too.
   - **2b. Call sites and boundary lines.** Replace the `fmt.Printf` at
     `engineio/transport/polling/server.go:145`; the parser, transport and dialer
     messages also become constants without a trailing colon, with `err` as an
     attribute; the attribute key `namespace` used by the connection code is renamed to
     `nsp` to match the table below. Expected shutdown paths (`io.EOF`, closed
     network connection) are `DEBUG`, not `ERROR`. Errors swallowed today get a `WARN`:
     `redis_broadcast.go` (`publish`, `publishMessage`, unmarshal in `onRequest` and
     `onResponse`, `dispatch` exit), `Server.serveError` when the namespace has no
     `OnError`, and the rejections in `engineio.Server.ServeHTTP`. `session.Session`
     records its open time and a close reason (`transport close` for a CLOSE packet,
     `ping timeout` for a deadline error, `transport error` for other I/O errors,
     `forced close` for `Close()` from the server; these are the Node strings) and logs
     it once. The boundary lines below are added. `TRACE` lines are guarded by
     `Enabled(ctx, LevelTrace)` and may carry the first 256 bytes of a payload; `DEBUG`
     never carries payloads. Per-packet and ping/pong lines are `TRACE` because 10k
     connections pinging every 20 s would flood `DEBUG`.

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

3. **Redis adapter races and hangs** (1 PR), `redis_broadcast.go`:
   - `requests` map written by `Len`/`AllRooms` and read by `dispatch` without a lock;
   - `onRequest` reads `bc.rooms` without `bc.lock`;
   - `Len`/`AllRooms` block on `<-req.done` forever if a peer dies: add a timeout;
   - `newNamespaceHandler` drops the `newRedisBroadcast` error, leaving `broadcast`
     nil and panicking on the first `Join`;
   - `dispatch` exits on `Receive` error with no reconnect: add backoff reconnect.
4. **Backpressure** (1 PR), `connection.go`: `writeChan`/`errorChan` are unbuffered and
   `broadcast.Send` holds `RLock` for the whole fan-out, so one stalled socket blocks a
   room. Add `Options.WriteBufferSize` (default 64); on overflow close the socket and
   call `OnError`; in `Send` snapshot the member list under `RLock`, emit without it.
5. **Small bugs, tests and links** (1-2 PRs). `session.Manager.Count` uses `Lock`
   instead of `RLock`; `engineio.Server.newSession` registers the session
   asynchronously after `InitSession`, so a fast second request with that `sid` gets
   HTTP 400 → register synchronously; `Server.Serve` returns nil on EOF; add tests for
   `engineio/session` (currently none) and for the root package (connect, event with
   ack, namespace, rooms, disconnect) using the existing Go client.
   Links and badges: `engineio/README.md` is cut to one purpose line plus links to the
   root `README.md` and `docs/PROTOCOL.md` (it is not in the ownership map and still
   carries the upstream `godoc.org` badge and text), and the `CLAUDE.md` ownership map
   gets a row for it. The root README GoDoc badge and API reference link keep the
   `googollee` module path until stage 2.5.

DoD: `make lint test-race` green on ubuntu/macos/windows for `stable` and `oldstable`;
`govulncheck` clean; two-instance Redis test under `-race` passes; slow-client test
proves other room members keep receiving; `engineio/session` coverage ≥ 70%, root
package ≥ 60%; `CHANGELOG.md` lists every fix with the issue or line it addresses.
Logging: `TestServerLoggerOption`, `TestLogLevelFromEnv`, `TestLogLevelInvalidEnv`,
`TestWrapOverridesHandlerLevel`, `TestTraceDisabledNoAlloc` and
`TestSessionCloseReason` pass; `TestNoBadKeyAttrs` runs the root scenario from task 5
at `trace` through a handler that fails on any `!BADKEY` attribute or non-constant
message, and asserts the same `sid` on session open, namespace connect, event and
disconnect; the godoc of package `logger` documents the variable, the levels and the
keys. Links: every badge in `README.md` shows the fork's status. Both greps below print
nothing:

```sh
grep -rnE '\b(log|fmt)\.Print' --include='*.go' . | grep -v '_examples/\|_test.go'
grep -rn 'godoc.org' --include='*.md' .
```

Acceptance: `_examples/default-http` works unchanged against `socket.io-client` 2.x;
`go get` of the fork at `v1.5.0` builds a consumer that previously used upstream (with a
`replace` directive). Owner runs `SOCKETIO_LOG_LEVEL=debug go run .` in
`_examples/default-http`, opens the browser page, sends one event and closes the tab:
the log shows session open, namespace connect, the event and a disconnect with
`reason="transport close"`, all with one `sid`. With `trace` the ping/pong and payload
lines appear; unset, only the pre-existing errors appear; `SOCKETIO_LOG_LEVEL=bogus`
prints one warning and behaves as unset. Every badge and link in `README.md` resolves
on GitHub.

## Stage 1b. Package layout (prerequisite to stage 2)

The first commits on `master` after `v1` is branched from `v1.5.0`; no tag. Moves with
`git mv` and import rewrites only: no behaviour change, no new features.

The root core (`conn`, `namespaceConn`, `namespaceHandler`, packet handlers, `Client`)
cannot be split into packages: `Conn` embeds `Namespace`, `namespaceConn` embeds
`*conn`, `conn` holds a map of `*namespaceConn`, `Broadcast` takes `Conn`, and
`(*conn).connectClient` is defined in `client.go`. That core is replaced by the new
model in 2.3 and is only renamed here.

Target tree (root module unless noted):

| Path | Package | Holds |
| --- | --- | --- |
| `.` | `socketio` | public API: `Server`, `Conn`, `Namespace`, `Client` (until 2.3), `Options` (from 2.4) |
| `adapter/` | `adapter` | `Broadcast` and `EachFunc` (v1), replaced by `Adapter` in 2.2; `adapter.Conn` with `ID` and `Emit`; in-memory implementation `adapter.NewMemory` |
| `adapter/codec/` | `codec` | 2.2, shared msgpack encoding |
| `adapter/redis/` | `redis` | v1 Redis broadcast; `redis.Options` (former `RedisAdapterOptions`), `redis.New`, `redis.Ping`; moves to module `adapters/redis` in 4b |
| `adaptertest/` | `adaptertest` | 4b |
| `client/` | `client` | Socket.IO client rewritten in 2.3; v1 `client.go` stays in the root until then |
| `contrib/otel/` | own module | 2.4 |
| `engineio/` | `engineio` | server side: `server.go`, `options.go` (from `server_options.go` and `types.go`), `conn.go` (from `connect.go`), `hooks.go` (2.4) |
| `engineio/client/` | `client` | `client.go`, `dialer.go` (`Dialer`, `Opener`); depends on `engineio.Conn` only |
| `engineio/session/` | `session` | `session.go`, `manager.go`, `id_generator.go`; `base.go` removed in favour of `frame.Type` |
| `engineio/frame`, `packet`, `payload`, `transport/...` | unchanged | |
| `parser/`, `logger/` | unchanged | |

1. **`engineio/client`** (1 PR). Move `engineio/client.go` and `engineio/dialer.go` to
   `engineio/client/`; `engineio.Dialer` becomes `client.Dialer`;
   `engineio/server_test.go` becomes package `engineio_test` importing
   `engineio/client`; the root `client.go` and `engineio/_examples` are updated.
2. **engineio file names and frame type** (1 PR). `connect.go` → `conn.go`; `types.go`
   merged into `options.go` (renamed from `server_options.go`); `session/base.go`
   removed, and `session.FrameType`, `session.TEXT`, `session.BINARY` are replaced by
   `frame.Type`, `frame.String`, `frame.Binary` in `engineio`, `engineio/client`,
   `parser` and root tests; `session_manager.go` → `manager.go`,
   `session_id_generator.go` → `id_generator.go`; the four `engineio/packet/fake_*.go`
   test doubles are merged into `engineio/packet/fake.go`.
3. **`adapter` and `adapter/redis`** (1 PR). `broadcast.go` → `adapter/memory.go`
   (`adapter.Broadcast`, `adapter.EachFunc`, `adapter.NewMemory`);
   `redis_broadcast.go`, `adapter_options.go` and `helpers.go` → `adapter/redis/`;
   the Redis dial in `Server.Adapter` moves to `redis.Ping`. The root keeps
   `type Broadcast = adapter.Broadcast` and
   `type RedisAdapterOptions = redis.Options` as deprecated aliases and keeps
   `Server.Adapter`; all three are removed in 2.2. `Server.ForEach` wraps the callback
   so the public `EachFunc func(Conn)` is unchanged.
4. **Root file names by role** (1 PR, `git mv` only). `connection_handlers.go` →
   `packet_handlers.go`; `namespace_handlers.go` merged into `namespace_handler.go`;
   `namespaces.go` merged into `connection.go`; `namespace_conn.go` → `namespace.go`;
   `handler.go` → `event_handler.go`; tests follow their files. The `CLAUDE.md` layout
   table is updated and stage 2 paths in this file point at the new tree.

DoD: `make lint test-race examples` green; `go vet ./...` clean;
`git diff -M90% --stat v1.5.0..HEAD -- '*.go'` shows every moved file as a rename;
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

### 2.1 Engine.IO v4 and gobwas/ws (3 PRs)

- Rewrite `engineio/payload` from the spec (the current length-prefix codec and
  `pauser` are the most tangled code in the repo).
- `engineio/session`: server ping ticker, deadline `pingInterval+pingTimeout`,
  `maxPayload`, noop on upgrade. `engineio/server.go`: `EIO` check, JSON errors.
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

### 2.2 Adapter interface (stage 4a, 1 PR)

```go
type Adapter interface {
    AddAll(sid SocketID, rooms []Room)
    Del(sid SocketID, room Room)
    DelAll(sid SocketID)
    Broadcast(ctx context.Context, pkt parser.Packet, opts BroadcastOptions) error
    Sockets(ctx context.Context, rooms []Room) ([]SocketID, error)
    SocketRooms(sid SocketID) []Room
    FetchSockets(ctx context.Context, opts BroadcastOptions) ([]RemoteSocket, error)
    ServerSideEmit(ctx context.Context, event string, args ...any) error
    Close() error
}
type BroadcastOptions struct{ Rooms, Except []Room; Flags BroadcastFlags }
type AdapterFactory func(nsp *Namespace) (Adapter, error)
```

In-memory implementation is the default. The current `Broadcast` interface and the
stage 1b aliases (`Broadcast`, `RedisAdapterOptions`, `Server.Adapter`) are removed.
The method set mirrors the Node adapter. Package `adapter/codec` in the root module holds
the msgpack encoding of broadcast messages shared by every backend adapter.

### 2.3 Socket.IO v5 and the generic API (4 PRs)

- `parser`: CONNECT payload, CONNECT_ERROR object, marker interface instead of
  `Type().Name()=="Buffer"`, `Packet` value type with lazily decoded args.
- New model `Server → Namespace → Socket` replacing `conn`/`namespaceConn`. Explicit
  CONNECT for `/`. Each `Socket` owns a `context.Context` cancelled on disconnect.
- Generics-first public API; reflection-based `OnEvent(string, interface{})` is
  removed:

```go
var Message = socketio.NewEvent[ChatMessage]("message")
var Send    = socketio.NewAckEvent[ChatMessage, Receipt]("send")

nsp.Handle(Message, func(ctx context.Context, s *socketio.Socket, m ChatMessage) error { ... })
nsp.Handle(Send, func(ctx context.Context, s *socketio.Socket, m ChatMessage) (Receipt, error) { ... })

Message.Emit(s, ChatMessage{...})
r, err := Send.EmitWithAck(ctx, s, ChatMessage{...})
nsp.To("room").Except(s.ID()).Emit(Message, ChatMessage{...})

nsp.Use(socketio.Auth[Credentials](func(ctx context.Context, s *socketio.Socket, c Credentials) error { ... }))
nsp.OnRaw(func(ctx context.Context, s *socketio.Socket, event string, args []json.RawMessage) error { ... })
```

- Handler errors are returned, not panicked; `Handle` returns an error on duplicate
  registration; sentinel errors (`ErrNamespaceClosed`, `ErrAckTimeout`,
  `ErrWriteBufferFull`, `ErrSocketClosed`) work with `errors.Is`. `ErrAckTimeout` is
  driven by `socketio.Options.AckTimeout` (default 30 s); every pending ack ends on
  disconnect with `ErrSocketClosed`, so each emit-with-ack has exactly one outcome.
  Multiple args per event are a struct or a tuple type `socketio.Args2[A, B]`; a `T`
  containing `socketio.Binary` carries binary attachments.
- `BenchmarkEventDispatch` (root) is added with the new model, so stage 2.4 has a real
  baseline.
- Rewrite `Client` on the same generic API with websocket over `gobwas/ws`.
- Migrate `_examples/*` to `socket.io-client@4` and the new API, and bump the pinned
  framework versions (gin 1.7.7, echo v3, gf v1, iris 12.1) to their current majors.

### 2.4 Observability (5 PRs)

Depends on 2.1 (server ping ticker, close reasons), 2.2 (`Adapter`) and 2.3 (`Socket`
context, typed events, `AckTimeout`). Tracing and metrics go through two hook structs in
the root module with no external dependency; the OpenTelemetry bridge is the separate
module `contrib/otel`. The debug and trace lines from stage 1.2 are produced by
`LoggingHooks`, the built-in implementation of the same structs, so whatever the logs
show can also be traced and measured, and that equivalence is tested.

```go
package engineio

type SessionInfo struct{ SID, Transport, RemoteAddr string }
type PacketInfo struct{ Type packet.Type; Frame frame.Type; Bytes int }
type CloseReason string // "transport close", "transport error", "ping timeout", "forced close", "server shutting down", "parse error"

type Hooks struct {
    HandshakeStart  func(ctx context.Context, r *http.Request) context.Context
    HandshakeEnd    func(ctx context.Context, s SessionInfo, err error, d time.Duration)
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
type EventResult struct{ Err error; AckWritten bool; Duration time.Duration }
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
    EmitStart       func(ctx context.Context, e EmitInfo) context.Context
    EmitEnd         func(ctx context.Context, e EmitInfo, err error, rtt time.Duration)
    BroadcastStart  func(ctx context.Context, b BroadcastInfo) context.Context
    BroadcastEnd    func(ctx context.Context, b BroadcastInfo, recipients int, err error)
    WriteBufferFull func(ctx context.Context, s SocketInfo)
    AdapterPublish  func(ctx context.Context, m AdapterMessage, err error)
    AdapterReceive  func(ctx context.Context, m AdapterMessage, err error)
}

func ChainHooks(hs ...*Hooks) *Hooks
func LoggingHooks(l *slog.Logger) *Hooks
func (n *Namespace) Hooks() *Hooks // nil-safe wrapper methods for adapters in other modules
```

1. **`engineio.Hooks`** (1 PR). `engineio.Options.Hooks`; the server installs
   `ChainHooks(LoggingHooks(log), opts.Hooks)`. Fire points: `Server.ServeHTTP`
   (`HandshakeStart` with `r.Context()` before the request checker, `RequestRejected`
   on each early return, `UpgradeStart` in the upgrade branch); session creation after
   the OPEN packet is written (`HandshakeEnd`, then `SessionOpen`, whose ctx replaces the
   `interface{}` context slot and is exposed as `Session.Context() context.Context`);
   session close (`SessionClose` with reason and duration); end of the upgrade
   (`UpgradeEnd`); the session's reader and writer are wrapped in a counting wrapper
   that fires `PacketRead` and `PacketWrite` on `Close` with the byte count, so
   transports need no hooks; the ping ticker fires `PingSent` and the PONG branch
   `PongReceived` with the RTT. Because `HandshakeStart` receives `r.Context()`, a span
   opened by router middleware (`otelgin`, `otelecho`) becomes the parent of the
   handshake span with no extra wiring. Hooks run on the goroutine that produced the
   event; they must not block, emit or panic (panics are not recovered, per the
   no-panics convention).
2. **`socketio.Hooks` and `socketio.Options`** (1 PR). `NewServer(*Options)`. Fire
   points: `ConnectStart` before the auth middleware and `ConnectEnd` after CONNECT or
   CONNECT_ERROR is queued; `Disconnect` with the Node reason strings; `EventStart`
   after the header decode and handler lookup, `EventEnd` after the handler returns and
   the ack is queued (decode errors and missing handlers are inside the span); `Emit`
   for fire-and-forget, `EmitStart` for emit-with-ack and exactly one `EmitEnd` from the
   ack packet, the ack timeout or disconnect; `BroadcastStart` and `BroadcastEnd`
   around `Adapter.Broadcast` with the recipient count from the in-memory adapter;
   `WriteBufferFull` before the socket is closed; `AdapterPublish` and
   `AdapterReceive` called by adapters through `Namespace.Hooks()`. The `Socket` ctx
   derives from the session ctx and is cancelled on disconnect; `EventStart` receives
   it and its result is what the handler gets.
3. **Dogfooding and overhead** (1 PR). The inline stage 1.2 lines are removed where a
   hook now exists, and `LoggingHooks` emits the same messages and keys, plus `rtt` on
   pong, `bytes` on packet, `rooms`, `except` and `local` on broadcast, and
   `socketio: adapter publish` / `socketio: adapter receive` at `TRACE`.
   `TestHooksCoverEveryHookPoint` (root) reflects over both structs, runs one scenario
   (handshake, upgrade, ping, connect with auth, event with ack, emit with ack,
   broadcast, buffer overflow, disconnect) with a recording hook chained after
   `LoggingHooks` at `trace`, and fails if any field was not called or has no log record
   with its message; adding a hook field without a log line therefore fails the build.
   `BenchmarkEventDispatch` gets the sub-benchmarks `no-hooks`,
   `logging-hooks-at-error` and `recording-hooks`.
4. **`contrib/otel`** (1 PR). Own `go.mod` (`.../contrib/otel/v2`), CI job and
   `README.md`. `otelsocketio.NewHooks(opts ...Option) (*engineio.Hooks,
   *socketio.Hooks)` with `WithTracerProvider`, `WithMeterProvider`,
   `WithPropagators`, `WithoutTraces`, `WithoutMetrics`, `WithEventAllowList`;
   `otelsocketio.NewSlogHandler(next slog.Handler) slog.Handler` adds `trace_id` and
   `span_id` from the record context. Trace context is extracted from the
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
   | `socketio.event {nsp} {event}` | Consumer | link: handshake; ends when the ack is queued | `sid`, `socket_id`, `nsp`, `event`, `ack_id`, `handler_found`, `error` |
   | `socketio.emit {nsp} {event}` | Producer | child of caller ctx; emit-with-ack only; ends on ack, timeout or disconnect | `sid`, `socket_id`, `nsp`, `event`, `ack_id`, `result` |
   | `socketio.broadcast {nsp} {event}` | Producer | child of caller ctx | `nsp`, `event`, `rooms.count`, `except.count`, `local`, `recipients` |
   | `socketio.adapter.publish {nsp}` | Producer | child of broadcast | `nsp`, `kind`, `bytes`, `error` |
   | `socketio.adapter.receive {nsp}` | Consumer | root: the Redis message format is fixed for Node compatibility and carries no trace context | `nsp`, `kind`, `bytes`, `error` |

   Instruments (Prometheus names replace dots with `_` and add unit suffixes):

   | Instrument | Kind, unit | Attributes | Hook |
   | --- | --- | --- | --- |
   | `engineio.handshakes` | Counter | `transport`, `result` (`ok`, `bad_transport`, `bad_sid`, `checker`, `accept`, `no_hijacker`, `init`) | HandshakeEnd, RequestRejected |
   | `engineio.handshake.duration` | Histogram, s | `transport`, `result` | HandshakeEnd |
   | `engineio.sessions` | UpDownCounter | `transport` | SessionOpen, SessionClose |
   | `engineio.session.duration` | Histogram, s | `transport`, `reason` | SessionClose |
   | `engineio.upgrades` | Counter | `from`, `to`, `result` | UpgradeEnd |
   | `engineio.packets` | Counter | `direction`, `type`, `transport` | PacketRead, PacketWrite |
   | `engineio.packet.size` | Histogram, By | `direction`, `transport` | PacketRead, PacketWrite |
   | `engineio.ping.rtt` | Histogram, s | `transport` | PongReceived |
   | `socketio.sockets` | UpDownCounter | `nsp` | ConnectEnd (ok), Disconnect |
   | `socketio.connects` | Counter | `nsp`, `result` (`ok`, `error`, `unknown_namespace`) | ConnectEnd |
   | `socketio.socket.duration` | Histogram, s | `nsp`, `reason` | Disconnect |
   | `socketio.events.received` | Counter | `nsp`, `event`, `result` (`ok`, `error`, `no_handler`, `decode_error`) | EventEnd |
   | `socketio.event.duration` | Histogram, s | `nsp`, `event` | EventEnd |
   | `socketio.events.sent` | Counter | `nsp`, `event` | Emit, EmitStart, BroadcastEnd (× recipients) |
   | `socketio.acks.pending` | UpDownCounter | `nsp` | EmitStart, EmitEnd |
   | `socketio.ack.rtt` | Histogram, s | `nsp`, `event`, `result` (`ok`, `timeout`, `closed`) | EmitEnd |
   | `socketio.broadcasts` | Counter | `nsp`, `local` | BroadcastEnd |
   | `socketio.broadcast.recipients` | Histogram, {socket} | `nsp` | BroadcastEnd |
   | `socketio.write_buffer.overflows` | Counter | `nsp` | WriteBufferFull |
   | `socketio.adapter.messages` | Counter | `nsp`, `direction`, `kind`, `result` | AdapterPublish, AdapterReceive |
   | `socketio.adapter.message.size` | Histogram, By | `nsp`, `direction` | AdapterPublish, AdapterReceive |

5. **Docs** (1 PR). `docs/OBSERVABILITY.md` owns `SOCKETIO_LOG_LEVEL`, the levels, the
   log keys, the hook contract (goroutine, non-blocking, no `Emit`, no panic recovery),
   the cardinality rule and the span and instrument catalogue. `CLAUDE.md` gets the
   ownership row and the `contrib/otel/` layout row; `README.md` gets one line linking
   to it. `TestObservabilityDocLists` (root) reflects over both `Hooks` structs and
   fails if a field name is missing from the doc.

DoD for 2.4: `TestHooksCoverEveryHookPoint`, `TestChainHooksOrder`, `TestNilHooks`,
`TestEmitWithAckEndsOnDisconnect`, `TestEmitWithAckTimeout`,
`TestSessionContextPropagates` and `TestObservabilityDocLists` pass under `-race`;
`benchstat` of `BenchmarkEventDispatch/no-hooks` between the last 2.3 commit and the
merge of 2.4 task 3 shows 0 added allocs/op and at most 2% more time;
`BenchmarkEventDispatchOtel/noop` is at most 4 allocs/op and 1 µs/op above `no-hooks`;
both results are recorded in `CHANGELOG.md`; `go mod graph` of the root module has no
`go.opentelemetry.io`. In `contrib/otel`: `TestSpans` asserts one span per row of the
span table with the listed attributes; `TestInstruments` asserts one series per
instrument; `TestMetricAttributesBounded` fails on `sid`, `socket_id`, `ack_id`,
`remote_addr` or an unregistered event in any metric attribute;
`TestHandshakeParentsUnderMiddlewareSpan` puts a recording span in the request context
and asserts the parent relation; `TestSlogHandlerAddsTraceID` passes.
`TestNoBadKeyAttrs` from stage 1 still passes at `trace`.

Acceptance for 2.4: owner runs the stage 3 chat with `SOCKETIO_LOG_LEVEL=debug`, an
OTLP collector and Jaeger from `docker compose`. A browser message appears as a
`socketio.event /chat message` span linked to that client's `engineio.handshake` span;
the log line for the same event carries the span's `trace_id`; `/metrics` shows
`socketio_events_received_total{nsp="/chat",event="message"}` incrementing and
`engineio_sessions` equal to the number of open tabs; closing a tab logs
`socketio: disconnect` with `reason="transport close"` and decrements the gauge.

### 2.5 Docs and release

`docs/MIGRATION.md`, `docs/PROTOCOL.md` update, `docs/OBSERVABILITY.md`,
`contrib/otel/README.md`, module path `.../v2`, tag `v2.0.0` and
`contrib/otel/v2.0.0` from the same commit; branch `v1` created from `v1.5.0`. After the
module path changes: README GoDoc badge and API reference link, `go.mod` and imports of
every `_examples/*`, links in `engineio/README.md`, and the import paths of
`contrib/otel` and `adapters/*`.

DoD: both official suites (`engine.io-protocol/test-suite`,
`socket.io-protocol/test-suite`) pass in CI against the Go server; a CI Node script with
real `socket.io-client@4` covers connect, namespace with auth, ack both ways, binary,
disconnect, reconnect after server restart; parser and payload unit tests cover every
example in the two specs; `go mod graph` shows no `gorilla/websocket`, and a lint rule
forbids `reflect` outside `parser`; all `_examples` build and run against
`socket.io-client@4`; `go vet`, lint, `-race` green; idle-connection benchmark numbers
recorded; `docs/PROTOCOL.md` lists every unimplemented item; the stage 2.4 DoD holds at
the tag. Router integration: the `examples` CI job starts `_examples/default-http`,
`gin-gonic`, `go-echo`, `iris` and `gf`, and `TestFrameworkSmoke` in `_examples/smoke`
(own `go.mod`) completes a websocket handshake and one event with ack through each with
the Go client; a test in `engineio` with a `ResponseWriter` that hides `http.Hijacker`
gets HTTP 501. Links: `pkg.go.dev/github.com/sshaplygin/go-socket.io/v2` renders the
tagged version, and the grep below matches only `CHANGELOG.md` (releases before the
fork) and the "Starting point" section of this file:

```sh
grep -rn 'googollee' --include='*.md' --include='go.mod' --include='*.go' .
```

Acceptance: a browser page on `socket.io-client@4` from CDN connects to
`_examples/default-http`, joins `/chat`, receives a typed ack, and the server logs a
clean disconnect on tab close. `docs/MIGRATION.md` is enough to port
`_examples/gin-gonic` without reading library code. A handler with a wrong payload type
fails at compile time. Every badge and link in `README.md` resolves to the v2 module.

## Stage 3. Realtime chat example (`_examples/chat/`, own go.mod)

- **Server**: namespace `/chat`; `Auth[Credentials]` middleware reading the nickname
  from the auth payload; rooms; typed events `Message` (ack returns id and timestamp),
  `Typing`, `History` (ring buffer of 50 per room), `Presence` on join/leave; direct
  messages via the socket-id room; binary image attachment; graceful shutdown;
  `/metrics` through `contrib/otel` and `otel/exporters/prometheus`.
- **Clients**: `index.html` with no build step on `socket.io-client@4` from CDN; Go CLI
  on the new `socketio.Client`; `cmd/load` (N Go clients, p50/p99 ack latency).
- **Cluster**: `docker-compose.yml` with two server instances, backend selected by
  `ADAPTER=redis|nats`, nginx `ip_hash`, an OTLP collector and Jaeger for the 2.4
  acceptance.
- **Test**: integration test starting the server and two Go clients, checking history
  and presence, run in the CI examples job.

DoD: `docker compose up` in `_examples/chat` starts in one command; a message sent to
instance A is shown by a client on instance B; `cmd/load` with 500 clients at 10 msg/s
reports p99 ack latency and no write-buffer overflow; integration test green in CI;
`_examples/chat/README.md` documents only how to run it.

Acceptance: owner runs the compose stack, opens two browser tabs on different
instances, exchanges messages, sees typing and presence, uploads an image, and kills one
instance without the other tab losing its session.

## Stage 4b. Independent adapters: Redis and NATS

Each adapter has its own `go.mod` and CI job and is tagged independently
(`adapters/redis/v2.0.0`, `adapters/nats/v2.0.0`). The root `go.mod` has no Redis or
NATS dependency. Both depend on the root module as a normal versioned dependency and on
`adapter/codec` for the message format.

- **`adapters/redis`**: wire-compatible with `@socket.io/redis-adapter` v8
  (non-sharded): channels `<prefix>#<nsp>#`, `<prefix>#<nsp>#<room>#`,
  `<prefix>-request#<nsp>#`, `<prefix>-response#<nsp>#`; broadcast messages msgpack
  `[uid, packet, opts]` via `vmihailenco/msgpack/v5` matching notepack output;
  request/response JSON as in Node; `redis.UniversalClient` injected; request timeout;
  subscriber reconnect with backoff.
- **`adapters/nats`**: subjects `<prefix>.<nsp>.broadcast` and
  `<prefix>.<nsp>.room.<room>`; same msgpack body; `Sockets`, `FetchSockets`,
  `ServerSideEmit` use NATS request-reply with a scatter-gather deadline; `*nats.Conn`
  injected; reconnect handled by the NATS client; tests on an embedded
  `nats-server/v2`, no Docker.
- Both adapters report through `Namespace.Hooks()` (`AdapterPublish`,
  `AdapterReceive`).
- **`adaptertest`** package in the root module: conformance suite any adapter runs
  against itself (like `fstest.TestFS`): join/leave, broadcast to room, except, local
  flag, fetch across two adapters, server-side emit, peer loss with timeout, and both
  adapter hooks firing.
- `docs/ADAPTERS.md`; `adapters/<name>/README.md` for backend options; chat example
  supports both backends.

DoD: `go mod graph` of the root module contains no redis or nats module; `adaptertest`
passes for in-memory, Redis and NATS; Redis suite (testcontainers, two servers) and NATS
suite (embedded server, two servers) pass under `-race`; cross-language CI test: one Go
server and one Node `socket.io@4` server with `@socket.io/redis-adapter` share Redis, a
room broadcast from each side reaches a client on the other, and `fetchSockets` from
Node lists the Go socket; msgpack fixtures captured from notepack decode byte-for-byte.

Acceptance: the chat cluster runs with one Go and one Node instance behind the same
nginx on Redis, and with two Go instances on NATS, and both tabs see each other's
messages in each configuration.

## Milestones

| Milestone | Content | Tag |
| --- | --- | --- |
| M0 | Stage 0 docs baseline | none |
| M1 | Stage 1 | `v1.5.0` |
| M1b | Stage 1b package layout | none (first commits after branch `v1`) |
| M2 | 2.1 Engine.IO v4 on gobwas/ws + conformance | branch `v2-dev` |
| M3 | 2.2 + 2.3 + 2.4 + 2.5 | `v2.0.0`, `contrib/otel/v2.0.0` |
| M4 | Stage 3 | `v2.1.0` |
| M5 | Stage 4b | `adapters/redis/v2.0.0`, `adapters/nats/v2.0.0` |

Effort: stage 2 is more than half of the total (payload codec, gobwas transport, the
generic Socket model, hooks at every layer). Stage 4b grows because of Node wire
compatibility and two backends.

## Out of scope

EIO=3 in v2; connection state recovery; WebTransport; permessage-deflate; sharded Redis
adapter (Redis 7 sharded pub/sub); NATS JetStream persistence; admin-ui protocol;
framework-specific integration packages (gin, echo, iris, gf use `http.Handler`); trace
context propagation through the Redis adapter.
