# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow SemVer.

## Unreleased

### Added

- `_experiments/adapter-wire`: a standalone module (not imported by the root module) with
  22 checked-in publications of the non-sharded Node Redis adapter
  (`@socket.io/redis-adapter@8.3.0`), their Go decode tests and a pinned Node oracle
  that reproduces them byte for byte. Test fixtures only; the library API and runtime
  are unchanged.
- `_experiments/adapter-rooms`: standalone module (not imported by the root module) with
  22 fixtures captured from the Node in-memory adapter (`socket.io-adapter` 2.5.5) for
  room membership and recipient selection, a Go fixture validator and a Node script that
  reproduces the fixtures; preparation for the stage 2.2 memory adapter, no change to the
  library.
- `engineio/payload/internal/eio4` and `engineio/transport/websocket/internal/eio4`:
  Engine.IO v4 polling payload codec (bounded body reads, exact `maxPayload` batching) and
  WebSocket packet codec, with fixtures, fuzz tests and pinned Node oracles
  (`engine.io-parser@5.2.3`, `engine.io-client@6.6.3`). Preparation for stage 2.1: no
  production code references them yet, so the Engine.IO v3 behaviour is unchanged.
- `_experiments/eio4-websocket`: standalone module (not imported by the root module) with a
  bounded `gobwas/ws` framing prototype and a Node `ws@8.18.3` peer that checks it; the root
  `go.mod` does not depend on `gobwas/ws`. Preparation for stage 2.1, no change to the
  library.
- `_experiments/sio5-codec`: standalone module (not imported by the root module) with a
  bounded Socket.IO protocol 5 wire codec (envelopes, complete binary groups, limits), Go
  tests, fuzz targets and a Node oracle pinned to `socket.io-parser` 4.2.7; preparation for
  the stage 2.3P parser, no change to the library.
- v2 API skeleton in the root package (roadmap 2.0): `Event[T]`, `AckEvent[T, R]`,
  `Args2`, `Binary`, `Endpoint`, `ClientRegistration`, raw handlers, `Server`,
  `Namespace`, `Socket`, `Options`, the `Adapter` contract with `AdapterFactory`
  (which takes `ctx`), the creating call `Server.Namespace(ctx, name)`, both hook
  structs and the runtime error sentinels. Declarations only: every operation that needs
  the runtime returns `ErrNotImplemented`, and `NewServer` creates no namespace.
  `engineio` gains `Hooks` and the `Hooks` and `PayloadPreviewBytes` options (with
  `Options.Normalize`); `parser` gains the `Packet`,
  `Arguments`, `BinaryValue` and `ArgumentCodec` value types. All additive.
- Compile fixtures in `go test`: a positive program and 20 negative programs that must
  fail to compile with recorded diagnostics (`internal/fixtures`, `testdata/negative`),
  and `make graph` / `TestPackageGraph` and `TestForbiddenEdge` for the package graph. The method-signature
  inventory is `docs/API.md`.
- G2 review of the skeleton (roadmap 2.0): `docs/API.md` records the frozen contract and what
  is explicitly not frozen; `ChainHooks` and `LoggingHooks` in `socketio` and `engineio`
  (the skeleton returns nil), `Server.ServeHTTP` (answers 501); `make freeze` and
  `make g2` run the gate checks. The `engineio` payload redactor type and
  `Options.PayloadRedactor` are not part of the skeleton: stage 2.4E defines the boundary.
  `LocalSockets` and `Namespace.LocalSockets` declare how an adapter delivers to local sockets.

### Changed

- CI: `make examples` only checks that every `_examples/*/chat.go` is identical, and
  `make vuln` no longer scans the `_examples` modules: the legacy examples build against
  the removed v1 runtime until stage 2.5D migrates them. The `lint` job runs
  `make graph`.
- CI: `make experiments` vets, format-checks, lints and race-tests every standalone
  `_experiments/*/go.mod` module in a new `experiments` job; `make vuln` and Dependabot
  (`gomod`, weekly) cover those modules too.
- CI: `make experiments` first checks that `_experiments` stays standalone (no root
  import, no `go.work`, every Go file under a `go.mod` of its own).

### Removed

- The v1 root runtime: the reflection-based `Server`, `Client`, namespace and handler
  API, the memory and Redis broadcast, `Server.Adapter` and the `redigo` dependency (and
  the test-only `miniredis` and the `uuid` dependency with it). The v1 code stays on the
  branch `v1.x`.

## v1.5.0 (unreleased, branch v1.x)

### Fixed

- engineio: when the write deadline passed (or the payload was closed) while the session
  writer was writing a polling response, `Payload.FlushOut` returned at once and the GET
  handler answered with `http.Error` on the same `http.ResponseWriter` the writer was
  still using, a data race under `-race` that could also append a second response to a
  partial one. `FlushOut` now returns only after the write in progress ends and rejects
  later writes, `FlushOut` also returns when the payload is closed instead of waiting for
  the deadline, and the handler no longer calls `http.Error` once the response has
  started (`engineio/transport/polling/server.go:121`, `engineio/payload/encoder.go:85`
  at `7ca5ca3`).
- engineio: the polling client could send its first poll while the open response was
  still being fed to the payload; that poll failed with "read: overlap", and the client
  stopped polling without an error, so reads waited until their deadline. The first poll
  now waits until the open response has been read
  (`engineio/transport/polling/connect.go:61`, `:260` at `7a7a71d`).
- engineio: a new session is registered before its OPEN packet is written, so a
  client that reuses the sid immediately no longer gets HTTP 400 "invalid sid";
  a session whose handshake fails is removed again (`engineio/server.go:173` at
  `61a7927`, roadmap task 1.S).
- `session.Manager.Count` takes the read lock instead of the write lock
  (`engineio/session/session_manager.go:50` at `61a7927`).
- `socketio.Server.Serve` returns nil instead of `io.EOF` after `Close`
  (`server.go:126` at `61a7927`).
- engineio: a request whose transport is earlier in the configured order than the
  session's current transport (for example polling after an upgrade to websocket) is
  answered with HTTP 400 instead of starting a second upgrade that held the request
  open until `pingTimeout`. This also removes a 60 s wait in `go test ./engineio`
  (`engineio/server.go:113` at `1feed4f`).
- Redis broadcast: every PUBLISH and PUBSUB command shared one redigo connection, which
  allows one caller at a time, so concurrent broadcasts and room queries could receive
  each other's replies; each command now takes a pooled connection
  (`redis_broadcast.go:91` at `312f77e`, roadmap task 1.R).
- Redis broadcast: the pending-request map of `Len` and `AllRooms` was read and written
  without a lock (`redis_broadcast.go:143`, `:274`, `:411` at `312f77e`).
- Redis broadcast: answers to peer requests counted room members without the room lock,
  and `Rooms(nil)` held the read lock across `AllRooms`, which deadlocked with a waiting
  writer (`redis_broadcast.go:290`, `:368` at `312f77e`).
- Redis broadcast: `Send`, `SendAll`, `ForEach` and delivered peer broadcasts emitted with
  the room lock held, so a connection leaving its rooms from `Emit` deadlocked the
  instance (`redis_broadcast.go:211`, `:226`, `:239`, `:471`, `:505` at `312f77e`).
- Redis broadcast: `Len` and `AllRooms` waited without a limit for a peer that never
  answers; they now return what arrived within 5 s (`redis_broadcast.go:149`, `:280` at
  `312f77e`).
- Redis broadcast: a failed construction left the connections it had opened open
  (`redis_broadcast.go:96`-`122` at `312f77e`).
- Redis broadcast: one malformed message or receive error stopped the subscriber for
  good, and some malformed messages panicked; the subscriber now skips them and
  reconnects with backoff (`redis_broadcast.go:551`, `:561` at `312f77e`).
- The in-memory broadcast's `Send`, `SendAll` and `ForEach` emitted with the room lock
  held, so a connection leaving its rooms from `Emit` deadlocked the namespace
  (`broadcast.go:87`, `:97`, `:109` at `48cf0d2`, roadmap task 1.B).
- engineio: `Server.Close` closed the channel that hands sessions to `Accept`, so a
  handshake completing around or after `Close` panicked with a send on a closed channel,
  and a session nobody accepted stayed open and counted. See
  [`engineio.Server.Close`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io/engineio#Server.Close)
  (`engineio/server.go:53`, `:178` at `79a393c`, roadmap task 1I).
- A namespace whose Redis broadcast could not be created held a nil broadcast, so every
  room call on it panicked, and the error was dropped. See
  [`Server.Adapter`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io#Server.Adapter)
  and [`Server.Serve`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io#Server.Serve)
  (`namespace_handler.go:27` at `79a393c`, roadmap task 1I).
- Concurrent handler registrations on one new namespace could each build a Redis
  broadcast and replace each other's handler (`server.go:346` at `79a393c`, roadmap
  task 1I).
- Building a namespace's Redis broadcast waited without a limit for a server that
  accepted the connection and never answered AUTH or SELECT, which with 1I also kept
  `Server.Close` waiting; both connections must now be dialled, AUTH and SELECT
  included, within 10 s (`redis_broadcast.go:119` at `79a393c`, roadmap task 1I).
- `Server.Close` left the Redis connections and subscriber goroutine of every namespace
  running. See [`Server.Close`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io#Server.Close)
  (`server.go:69` at `79a393c`, roadmap task 1I).
- engineio: after an upgrade switch the session kept the deadline set for the upgrade
  probe on the new connection, so `PingTimeout` ran from the probe instead of the switch;
  the session now sets the new connection's deadline again and closes as
  `transport error` if that fails (`engineio/session/session.go:483` at `1151bad`,
  roadmap task 1.L).
- engineio: a websocket handshake error at session creation was answered twice: the
  websocket library had already written its 400 and `http.Error` followed, so net/http
  logged a superfluous `WriteHeader`. That path now skips `http.Error`, as the upgrade
  path already did (`engineio/server.go:134` at `1151bad`, roadmap task 1.L).

### Added

- `engineio.Options.Logger` (`*slog.Logger`): the Engine.IO server, its sessions, the
  socket.io `Server`, `Client` and every connection log through it; nil means
  `logger.Log`. The parser, the transports, `engineio/packet` and the client dialer still
  use the package-level `logger.Log` (roadmap stage 1.2).
- `SOCKETIO_LOG_LEVEL` (`error`, `warn`, `info`, `debug`, `trace`), read once at start
  into `logger.Level`: while set, it decides which library records are enabled,
  whatever the level of the application's handler. Applications can also call
  `logger.Level.Set` at runtime; `logger.LevelUnset` hands the decision back to the
  handler. `logger.Wrap`, `logger.LevelTrace` and `logger.ReplaceAttr` (renders
  `TRACE`) are exported. Connection and session records carry `sid`; session records
  also carry the current `transport`, updated on upgrade (roadmap stage 1.2a).
- `logger.Log` writes to whatever `slog.Default()` is at log time, so an application's
  `slog.SetDefault` in `main` applies to library records.
- engineio: each session of `engineio.Server` logs `engineio: session open` (DEBUG,
  `sid`, `transport`, `remote_addr`) after its handshake and, exactly once, `engineio:
  session close` (DEBUG, `sid`, `transport`, `reason`, `duration`, and `err` for a
  transport error). `reason` is the first cause the session observed: `transport close`
  (CLOSE packet from the client), `ping timeout`, `transport error` (any other read or
  write failure, including a peer close), `forced close` (`Close`) or `server shutting
  down` (`engineio.Server.Close` before `Accept`) (roadmap task 1.L).
- engineio: `engineio: request rejected` (WARN; DEBUG for `unknown sid`) with
  `transport`, `remote_addr`, `reason` and `err`, once per request `ServeHTTP` rejects and
  once per failed session initialisation. `reason` is `bad transport`, `checker`,
  `unknown sid`, `accept`, `init` or `bad upgrade`; the failed initialisation was logged
  as `init new session` at ERROR (roadmap task 1.L).
- `socketio: unhandled error` (WARN, `sid`, `nsp`, `err`): an error that no `OnError`
  receives, such as a CONNECT to a namespace without handlers, a decode or dispatch
  error, a marshal error in `Encode` or an overflow (`nsp` is the overflowing packet's
  namespace), was dropped silently. It is logged once unless it is expected closure: a
  failure of the engine.io frame reader or writer that the parser returned, a peer close
  or a ping timeout, or any failure after a close started (roadmap task 1.L).
- `socketio.Server` connections log `socketio: namespace connect` (DEBUG, `sid`, `nsp`, and
  `err` when `OnConnect` or the connect failed; an overflow during root `OnConnect` gives
  `ErrWriteBufferFull`, joined with the `OnConnect` error) and, once per connected
  namespace, `socketio: disconnect` (DEBUG, `sid`, `nsp`, `reason` `namespace disconnect`
  or `connection close`; text sent by the peer is not logged). `Client` logs neither
  (roadmap task 1.L).
- `engineio.Options.WriteBufferSize` (temporary v1 placement) and `ErrWriteBufferFull`:
  each connection queues at most that many outbound packets (default 64). See
  [`engineio.Options`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io/engineio#Options)
  and [`ErrWriteBufferFull`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io#ErrWriteBufferFull)
  (roadmap tasks 1.B and 1I).

### Changed

- CI: run `govulncheck` in the lint job with the newest Go release published by go.dev,
  because the `setup-go` manifest can lag and report fixed standard-library vulnerabilities.
- CI: a `min-go` job builds and race-tests the root module on Ubuntu with Go 1.22
  and `GOTOOLCHAIN=local`, so a `go.mod` or dependency that requires a newer Go
  fails CI instead of downloading a newer toolchain (stage 1 DoD).
- CI: compare PR benchmarks against the base with `benchstat`, preserving reports
  and raw measurements; skip ordinary comment-only and documentation changes and
  remove the unconditional benchmark runs from the test matrix.
- Benchmark reports use separate Markdown timing tables per package with
  percentage changes and advisory ±20% markers; full `benchstat` results are
  available in a collapsible section.
- The v1 module and its internal imports use `github.com/sshaplygin/go-socket.io`.
  Consumers must change upstream-path imports and remove the old `replace`
  directive; README installation instructions use the fork directly. Examples
  now all build against the checked-out fork, including the Docker example.
- `session.New` takes a trailing `*slog.Logger` parameter (nil accepted).
- `logger.Error` accepts a nil error instead of panicking.
- Connection-level errors that were printed with `log.Println` are now `slog` Error
  records carrying the namespace. Messages logged by the Engine.IO session and the
  socket.io server, client and connection code lose their trailing colons; messages
  from the parser, the transports and the dialer are unchanged.
- Toolchain: `go 1.22` in `go.mod`; `golang.org/x/exp/slog` replaced by `log/slog`;
  `gofrs/uuid` replaced by `google/uuid`; `gorilla/websocket` 1.5.3; `testify` 1.12.1;
  `io/ioutil` replaced by `io` (roadmap stage 1.1).
- Lint: `.golangci.yml` migrated to the golangci-lint v2 schema with the standard
  linter set; `EmptyAddrErr` renamed to `ErrEmptyAddr` with the old name kept
  as a deprecated alias.
- Build: Makefile targets `test`, `test-race`, `bench`, `lint`, `vuln`, `cover`,
  `examples`; CI split into `lint`, `test` (3 OS × 2 Go) and `examples` jobs on
  current GitHub Actions; Dependabot updates grouped weekly.
- Examples: all `_examples/*` modules tidied; nine of them did not build before.
- Documentation baseline: English-only docs with a single owner per topic
  (`CLAUDE.md`, `docs/ROADMAP.md`, `docs/PROTOCOL.md`); `README.md` trimmed to
  purpose, compatibility, install and quick start; `upgrade workflow.md` merged into
  `docs/PROTOCOL.md`.
- `engineio/README.md` only says what the package is and links to `README.md`,
  `docs/PROTOCOL.md` and the godoc; its install command and its example, which used
  `io/ioutil` and ignored the errors of `NextReader`, `ReadAll` and `NextWriter`, are
  removed (roadmap task 1.D).
- `logger/README.md` is removed. It told applications to assign `logger.Log`, which
  replaces the handler that applies `logger.Level`, so `SOCKETIO_LOG_LEVEL` and
  `logger.Level.Set` do not apply to records logged through it.
  `engineio.Options.Logger` routes the records of the socket.io server, client and
  connections and of the Engine.IO server and its sessions; the parser, the
  transports, `engineio/packet` and the client dialer log through `logger.Log`, which
  follows `slog.SetDefault`. The `logger` package godoc documents it (roadmap task
  1.D).
- Backpressure: `Emit` never blocks; it blocked until the writer took the packet
  (`connection.go:136` at `48cf0d2`). A connection whose queue overflows is closed
  without draining and reports `ErrWriteBufferFull`; more than `WriteBufferSize` packets
  queued faster than they are written can close a healthy client, polling ones much
  sooner. See [`Namespace.Emit`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io#Namespace)
  (roadmap task 1.B).
- `Conn.Close` and `Client.Close` drain: they run `OnDisconnect` and return, the packets
  queued until then are written in the background, and the engine.io connection closes
  when they are written or after `engineio.Options.PingTimeout`; `Server.Count` counts the
  session until then. They closed engine.io at once (`connection.go:66` at `48cf0d2`).
  Closes started by the library (read, decode, dispatch or encode error, peer close, ping
  timeout, overflow, failed connect) discard the queue. See
  [`Conn`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io#Conn) (roadmap tasks 1.B
  and 1I).
- An encode error now closes the connection; it used to leave it open
  (`server.go:309` at `48cf0d2`).
- The error that closes a connection is reported to `OnError` before the close's effects
  run; a failed connect was closed before it was reported (`server.go:249` at `48cf0d2`).
- The warning for an invalid `SOCKETIO_LOG_LEVEL` is logged as
  `logger: invalid level ignored` with the value under `value`; it read
  `logger: invalid SOCKETIO_LOG_LEVEL, ignored` (`logger/logger.go:67` at `1151bad`,
  roadmap task 1.L).
- The polling transport logs a POST with an unsupported `Content-Type`, a failed payload
  read and a failed answer at DEBUG instead of ERROR, and no longer prints the answer
  failure with `fmt.Printf`: the client gets the 400 or has gone
  (`engineio/transport/polling/server.go:131`, `:137`, `:144`, `:145` at `1151bad`,
  roadmap task 1.L).
- socket.io and parser records: messages are constants such as
  `socketio: event decode failed`; the `namespace` key is `nsp` (the root namespace is
  `/`), `id` is `ack_id` and `argTypes` is no longer logged. Records of an error that is
  also reported to `OnError`, logged as unhandled or returned, and the parser's frame
  failures, are DEBUG instead of ERROR or INFO; an emit before the client's namespace is
  connected, an ACK callback of the wrong type and an EVENT for a namespace without
  handler are WARN instead of INFO (`connection_handlers.go:27`-`:216`, `server.go:348`,
  `:371`, `client.go:117`, `parser/decoder.go:333`, `parser/encoder.go:31`-`:209` at
  `1151bad`, roadmap task 1.L).
- engineio records: the session, the upgrade probe, the engine.io client and dialer,
  the polling client, the packet encoder and the websocket wrapper logged through
  `logger.Error` at ERROR with free-form messages such as `getOpen store 2:`. They now
  log constant `engineio: ...` messages with an `err` key: DEBUG for failures after a
  close started, session frame failures reported as close reasons, upgrade-probe
  failures, errors also returned to a caller (including the polling client's stored
  request failures and the engine.io client's reader `Close` failures, which its next
  `NextReader` returns), the engine.io client's ping failures that are expected closure
  (its `Close` has started, `io.EOF`, a closed or lost connection, a websocket close
  frame from the peer, or a polling request failure after which the transport closed
  itself) and the websocket "frame not
  closed" reminders, which no longer carry a synthetic `ConnectionNotClosed` error;
  WARN for failures no caller receives: the engine.io client's other ping failures,
  the dialer's and the polling client's reader `Close` failures during the handshake,
  and `engineio: transport dial failed` (with
  `transport`) for a transport attempt whose error `Dial` does not return (the last
  attempt is DEBUG) (`engineio/session/session.go:56`-`:507`, `engineio/client.go:70`-`:133`,
  `engineio/dialer.go:23`-`:87`, `engineio/transport/polling/connect.go:38`-`:277`,
  `engineio/packet/encoder.go:40`, `engineio/transport/websocket/wrapper.go:66`, `:131`
  at `1151bad`, roadmap task 1.L).
- Examples: the old `notice`/`msg`/`bye`/`echo` demo is replaced in every example by a port of the
  Socket.IO chat example (`socket.io-client` 2.5.0 page in `_examples/asset/`, server logic in `chat.go`).

### Deprecated

- `logger.Error` and `logger.Info`: the library no longer calls them. Use `slog`'s
  methods on `logger.Log` or on the logger passed as `engineio.Options.Logger`. See
  [`logger`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io/logger), whose
  godoc now documents the levels, the message pattern and the attribute keys of library
  records (roadmap task 1.L).

### Known limitations

- Redis broadcast: handler registration does not wait until Redis has registered the
  namespace's subscription, and a lost subscription is reopened later, so until Redis
  has registered it the namespace on that instance misses other instances' broadcasts
  and room requests, and room queries on every instance, its own included, leave out
  its connections (see
  [`Server.Adapter`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io#Server.Adapter)).
- Redis broadcast: `Server.RoomLen` and `Server.Rooms` wait the full 5 s and return the
  answers received by then when the requesting instance has not yet registered its
  subscription or an instance that Redis counts does not answer (`Rooms` also when Redis
  cannot report that count), so they can undercount or omit rooms (see
  [`Server.RoomLen`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io#Server.RoomLen)
  and [`Server.Rooms`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io#Server.Rooms)).

## v1.4.2 and earlier

See the upstream release notes at
<https://github.com/googollee/go-socket.io/releases>.
