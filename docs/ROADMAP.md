# Roadmap

Status: approved 2026-09-28. Owner: Sam Shaplygin.

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

## Stage order

1 → 2 → 3 → 4. The adapter interface (4a) is fixed at the start of stage 2 because the
namespace rewrite for protocol v5 touches the broadcast layer anyway. Moving Redis and
NATS into their own modules (4b) follows stage 3.

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

No protocol or public API changes except `engineio.Options.Logger`. `gorilla/websocket`
stays in v1; the transport swap happens in stage 2 where the transport is rewritten.

1. **Toolchain and CI** (1 PR). `go.mod` → `go 1.22`; drop `golang.org/x/exp`,
   `io/ioutil`, `gofrs/uuid+incompatible` → `google/uuid`; bump `gorilla/websocket`
   to 1.5.3 and `testify`. `.golangci.yml` → v2 format. `ci.yaml`: `checkout@v4`,
   `setup-go@v5`, `golangci-lint-action@v8`, a job building every `_examples/*`,
   `govulncheck`. Makefile targets: `lint test test-race cover bench examples`.
2. **Logger** (1 PR). `logger/logger.go` → `log/slog`; add `engineio.Options.Logger`;
   remove the remaining `log.Println` calls in `connection_handlers.go` and
   `engineio/server.go`.
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
5. **Small bugs and tests** (1-2 PRs). `session.Manager.Count` uses `Lock` instead of
   `RLock`; `engineio.Server.newSession` registers the session asynchronously after
   `InitSession`, so a fast second request with that `sid` gets HTTP 400 → register
   synchronously; `Server.Serve` returns nil on EOF; add tests for `engineio/session`
   (currently none) and for the root package (connect, event with ack, namespace,
   rooms, disconnect) using the existing Go client.

DoD: `make lint test-race` green on ubuntu/macos/windows for `stable` and `oldstable`;
`govulncheck` clean; two-instance Redis test under `-race` passes; slow-client test
proves other room members keep receiving; `engineio/session` coverage ≥ 70%, root
package ≥ 60%; `CHANGELOG.md` lists every fix with the issue or line it addresses.

Acceptance: `_examples/default-http` works unchanged against `socket.io-client` 2.x;
`go get` of the fork at `v1.5.0` builds a consumer that previously used upstream (with a
`replace` directive).

## Stage 2. Socket.IO protocol v5 over Engine.IO protocol v4 (tag `v2.0.0`)

Protocol deltas are listed in [PROTOCOL.md](PROTOCOL.md#planned-engineio-v4-and-socketio-v5).

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
  `ws.Dialer`. `gorilla/websocket` removed from `go.mod`.
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

In-memory implementation is the default. The current `Broadcast` interface is removed.
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
  `ErrWriteBufferFull`) work with `errors.Is`. Multiple args per event are a struct or
  a tuple type `socketio.Args2[A, B]`; a `T` containing `socketio.Binary` carries
  binary attachments.
- Rewrite `Client` on the same generic API with websocket over `gobwas/ws`.
- Migrate `_examples/*` to `socket.io-client@4` and the new API.

### 2.4 Docs and release

`docs/MIGRATION.md`, `docs/PROTOCOL.md` update, module path `.../v2`, tag `v2.0.0`;
branch `v1` created from `v1.5.0`.

DoD: both official suites (`engine.io-protocol/test-suite`,
`socket.io-protocol/test-suite`) pass in CI against the Go server; a CI Node script with
real `socket.io-client@4` covers connect, namespace with auth, ack both ways, binary,
disconnect, reconnect after server restart; parser and payload unit tests cover every
example in the two specs; `go mod graph` shows no `gorilla/websocket`, and a lint rule
forbids `reflect` outside `parser`; all `_examples` build and run against
`socket.io-client@4`; `go vet`, lint, `-race` green; idle-connection benchmark numbers
recorded; `docs/PROTOCOL.md` lists every unimplemented item.

Acceptance: a browser page on `socket.io-client@4` from CDN connects to
`_examples/default-http`, joins `/chat`, receives a typed ack, and the server logs a
clean disconnect on tab close. `docs/MIGRATION.md` is enough to port
`_examples/gin-gonic` without reading library code. A handler with a wrong payload type
fails at compile time.

## Stage 3. Realtime chat example (`_examples/chat/`, own go.mod)

- **Server**: namespace `/chat`; `Auth[Credentials]` middleware reading the nickname
  from the auth payload; rooms; typed events `Message` (ack returns id and timestamp),
  `Typing`, `History` (ring buffer of 50 per room), `Presence` on join/leave; direct
  messages via the socket-id room; binary image attachment; graceful shutdown;
  `/metrics` (Prometheus).
- **Clients**: `index.html` with no build step on `socket.io-client@4` from CDN; Go CLI
  on the new `socketio.Client`; `cmd/load` (N Go clients, p50/p99 ack latency).
- **Cluster**: `docker-compose.yml` with two server instances, backend selected by
  `ADAPTER=redis|nats`, nginx `ip_hash`.
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
- **`adaptertest`** package in the root module: conformance suite any adapter runs
  against itself (like `fstest.TestFS`): join/leave, broadcast to room, except, local
  flag, fetch across two adapters, server-side emit, peer loss with timeout.
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
| M2 | 2.1 Engine.IO v4 on gobwas/ws + conformance | branch `v2-dev` |
| M3 | 2.2 + 2.3 + 2.4 | `v2.0.0` |
| M4 | Stage 3 | `v2.1.0` |
| M5 | Stage 4b | `adapters/redis/v2.0.0`, `adapters/nats/v2.0.0` |

Effort: stage 2 is more than half of the total (payload codec, gobwas transport, the
generic Socket model). Stage 4b grows because of Node wire compatibility and two
backends.

## Out of scope

EIO=3 in v2; connection state recovery; WebTransport; permessage-deflate; sharded Redis
adapter (Redis 7 sharded pub/sub); NATS JetStream persistence; admin-ui protocol.
