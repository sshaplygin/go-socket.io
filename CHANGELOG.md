# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow SemVer.

## Unreleased

### Fixed

- V1 examples return errors instead of terminating the process, supervise HTTP and
  Socket.IO serving together, and release listeners/connections before returning.
  Signal shutdown retains the graceful example's delay; service failures bypass it.
  Engine.IO echo failures close only the affected peer and all acquired I/O. A
  portable source gate covers library/example termination calls and the README,
  including CRLF, mixed line endings and Markdown Go blocks parsed through an AST;
  dot imports cannot hide a terminating API from the gate.

- tests: synchronize the compressed-limit CLOSE assertion with the error hook so
  a Windows TCP reset cannot hide CLOSE 1009 after a pipelined packet.

- engineio/parser: a decoding or source-release failure terminates the streaming
  payload decoder, preventing reuse and repeated release of the failed source.
  Body read errors are retained on their first observation, including a read that
  returns data together with an error.

- engineio: orderly polling close drains admitted frames before sending CLOSE
  through the pending or next poll, with a 30 second bound. Forced root stops and
  server shutdown interrupt the wait immediately; upgrade cleanup stays immediate.

- engineio: closing a polling payload after its reader was acquired releases
  FeedIn instead of leaving the polling client goroutine waiting forever.

- socketio: `Server.Close` waits for active session handlers to finish, with a
  10 second bound and a warning that reports the number still open.

- socketio: unknown namespaces receive ERROR "Invalid namespace" without losing the
  session. ERROR packets reach OnError, namespace CONNECT replies use `0/nsp,`, and
  the parser writes namespace DISCONNECT packets with their trailing comma.
- socketio: JSON encoding finishes before any frame is written. An encode failure
  reports OnError and sends ERROR "encode error" while keeping the connection usable.

- parser: reject more than 10 binary attachments before allocation or reads, validate
  JSON payloads by packet type, and reject invalid binary placeholder indices.
  Decode complete event names, including commas, without splitting JSON text.
- socketio: `Emit(event, nil)` sends a JSON null instead of panicking. Event and ACK
  argument type mismatches are reported to `OnError` and leave the session open;
  binary attachments are consumed before conversion so the next packet stays aligned.
- memory broadcast: `BroadcastToNamespace` sends one copy per socket regardless of
  how many rooms it joined, using a recipient snapshot outside the room lock.

- engineio: default session IDs use cryptographic randomness instead of a sequential
  counter. `Server.Close` closes and removes accepted sessions as well as pending ones.
- engineio: polling POST bodies and websocket messages have a configurable byte limit;
  excessive POSTs return HTTP 413 and close the transport, excessive websocket messages
  close with code 1009. Upgrade probes have a separate timeout and leave polling usable
  when they fail. `ConnInitor` runs before OPEN and Accept on both transports.

- engineio: a binary polling payload (the default when the client does not send `b64=1`)
  gave each packet the UTF-16 code-unit length of the text payload instead of its length in
  bytes. A client that polls without `b64=1` (the browser build of `socket.io-client` 2.x
  with XHR2; in Node, engine.io-client sends `b64=1` and gets the text payload) therefore
  failed the poll with a parser error on any non-ASCII text, and its POST holding a
  non-ASCII string packet was read past its end; reproduced with engine.io-parser 2.2.1,
  not with a browser. Lengths
  in a binary payload now count bytes, as engine.io-parser 2.2.1 does; the text payload
  keeps UTF-16 code units (`engineio/payload/encoder.go:144`, `decoder.go:55` at
  `18af32b`).
- parser: the encoder wrote the placeholder number and the binary flag into the caller's
  `*parser.Buffer`, so one `*Buffer` in the arguments of a broadcast was written concurrently
  by the encoders of several connections (a data race under `-race`), and the same `*Buffer`
  twice in a packet got one number for both placeholders. The encoder now numbers private
  copies and leaves the arguments untouched. A `*Buffer` behind an unexported field (an
  embedded unexported slice type included; both panicked in `reflect` before) or an embedded
  pointer to an unexported struct (it worked, by writing into the Buffer) makes `Encode`
  return an error; an embedded unexported struct value with an exported `*Buffer` field works.
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

- engineio: optional websocket permessage-deflate, default polling gzip/deflate
  compression and configurable session cookies. Compression thresholds include the
  encoded packet; websocket read limits also cap inflated messages with code 1009.
- socketio/client: immutable `Compress(bool)` emitters carry the per-packet flag
  through queued events and binary attachments. Broadcasts honour it locally;
  the legacy Redis wire format does not carry it to other instances.

- examples: add namespaces/rooms/auth and middleware modules with executable
  Node checks for both supported clients. Chat examples use broadcasts excluding
  the sender; Redis examples retain their local helper. Serve a pinned 1.7.4
  browser page beside the 2.5.0 page.


- client: recover engine connections automatically with configurable retry limits,
  backoff delays and reconnect callbacks. Closed namespaces stay closed on recovery;
  ordinary emits buffer while reconnecting and volatile emits drop before CONNECT.
- client: expose namespace ID/Connected, On/Once/Off listeners and ping/pong observers.
  Namespace.Connected is available on server connections too; engineio/client.Dialer gains
  optional heartbeat observers.


- client: add `New` options and namespace sockets sharing one Engine.IO connection,
  connect timeout and retries, pre-connect event buffering, and namespace DISCONNECT
  on close. Engine path, query and headers apply to both transports and upgrade.

- engineio: optional ordered transport upgrades keep polling usable until a
  successful websocket probe, preserve packet order, and bound failed probes.
  Engine clients expose their first close reason through `CloseError`.

- Differential conformance scenarios compare Go and Node event, ACK, binary,
  namespace, handshake and disconnect transcripts under both supported clients.

- Executable ACK and binary examples, plus a Node chat-login check. `make examples`
  vets standalone modules; `make examples-node` checks each client script under both
  supported Node clients in CI.

- The Go client is available as `github.com/sshaplygin/go-socket.io/client`,
  with its own client connection interfaces.

- ci: `parity-check` verifies PARITY markers against executable tests, including
  tagged scenarios. `cover-check` enforces measured statement and API coverage ratchets.

- ci: a tagged conformance suite runs Socket.IO clients 1.7.4 and 2.5.0 against
  the Go server over polling and websocket, including upgrade and reconnect.
  Raw HTTP and websocket tests check OPTIONS, framing, close and overlapping requests.

- socketio: `SetAdapter` injects namespace adapters, and `OfFunc` creates dynamic
  namespaces with inherited registrations and parent broadcasts. Namespaces expose
  their adapter and support ordered `On`, `Once`, and `Off` listeners.
  Parent broadcasts stop on the first child adapter error; earlier deliveries remain.
- socketio: `Conn.EngineConn`, `Request`, and `Handshake` expose original handshake
  metadata, including namespace query overrides, across transport upgrades and close.
  A final `AckFunc` handler parameter supports one asynchronous acknowledgement.

- engineio: `WithCORS(false)` restores polling without CORS headers and
  websocket same-origin checks. Explicit transport `CheckOrigin` callbacks override
  both settings; resolving the server default copies built-in transports.

- socketio: immutable Emitter builders select room unions and socket exclusions,
  with volatile and local flags. Namespace sockets can join several rooms and
  broadcast except their sender; adapters and namespaces expose socket ID snapshots.
  New broadcast entry points reject acknowledgement callbacks with ErrBroadcastAck;
  legacy broadcasts retain one callback per socket. Redis exclusions return
  ErrExceptUnsupported without delivery, and unknown message extensions are dropped.

- engineio: optional Hooks observe connection, initial and per-response headers,
  connection errors, packets, heartbeat, upgrades, polling flush/drain and transport
  errors. Callbacks run synchronously; header hooks may edit response headers.

- socketio: `Server.Of` returns a stable `ServerNamespace` with handler registration,
  namespace middleware, `Emit`, `Send`, and connected-socket snapshots. `Server.Use`
  registers middleware through the same namespace. `Namespace` gains `Send`,
  `Disconnect`, and event `Use`; client-side event middleware registers nothing.

- engineio: expose connection ReadyState, Transport and Upgraded, server Clients
  snapshots and socketio Server.Engine. Clients includes open and closing sessions;
  Count retains the registered-session lifetime until Remove.

- engineio: CloseError exposes the first session close cause through NextReader and
  NextWriter; its underlying error remains available through errors.Is/As. Sessions,
  engine clients and their transports report optional Writable state for volatile sends.

- Development: `make sync-check` and an unfiltered CI check guard shared v1/v2
  foundation files against drift after rewriting the module import path.

- engineio: `WithUpgradeTimeout` (10 seconds), `WithMaxHTTPBufferSize` (1,000,000 bytes)
  and `WithUpgrades(false)`; nonpositive limits use the defaults. The server configures
  private sessions before `ConnInitor` receives the original handshake request and
  connection. Transports expose `SetReadLimit`; private polling state can answer an
  idle poll during upgrade.

- `engineio.WithLogger` (`*slog.Logger`) routes Engine.IO server/session records;
  `socketio.WithLogger` configures both Socket.IO and its engine. Socket.IO clients
  and their connections use `client.WithLogger`. A nil logger selects `logger.Log`.
  Both parsers, transports and Engine.IO client/dialer use package-level `logger.Log`
  (roadmap stage 1.2).
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
- Socket.IO server/client `WithWriteBufferSize` and role-specific `ErrWriteBufferFull`:
  each connection queues at most that many outbound packets (default 64). See
  [`socketio.WithWriteBufferSize`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io#WithWriteBufferSize)
  and [`client.WithWriteBufferSize`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io/client#WithWriteBufferSize)
  and [`ErrWriteBufferFull`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io#ErrWriteBufferFull)
  (roadmap tasks 1.B and 1I).

### Changed

- Public constructors use targeted functional options: default servers are
  `NewServer()` and clients are `client.New(addr)`. `With…` functions configure
  their owning role, in order, without sharing per-instance configuration.
  Required codec streams and client addresses remain positional. See
  [the v1 migration guide](docs/V1_MIGRATION.md) for field-to-option mappings.

- Server and client now have independent connection contracts. Socket.IO queue/drain
  options belong to their own role; Engine.IO server/client share only wire codecs
  and transports. The Socket.IO parser owns its frame type and imports no Engine.IO
  runtime. Wrong-role event callbacks are rejected at registration.

- engineio: shared wire codecs and OPEN parameters now live in `engineio/parser`;
  polling owns its feed/flush and pause/deadline state. The former frame, packet,
  payload and transport-utils import paths are removed. See
  [the v1 migration guide](docs/V1_MIGRATION.md) for the new imports and API.

- v1 source layout is organized by responsibility. The private server session owns
  abort, shutdown and response-header observation, replacing packages with init-time
  function registration. Orderly close, forced close and hook ordering retain their
  behavior. Imports and role contracts change; see the migration guide.

- v1 migration: this minor release expands `Conn`, `Namespace`, `Broadcast` and
  `engineio.Conn`, breaking custom implementations and mocks. `Conn` gains
  `EngineConn`, `Request` and `Handshake`; its embedded `Namespace` gains
  `Send`, `Disconnect`, `Use`, `To`, `Broadcast`, `Volatile`, `Connected` and
  `Compress`. `Namespace.Join(room string)` changes to `Join(rooms ...string)`:
  existing single-room calls compile unchanged, but implementations must adopt
  the variadic signature. `Broadcast` gains `SendTo` and `IDs`;
  `engineio.Conn` gains `ReadyState`, `Transport` and `Upgraded`. Add these
  methods to implementations, or embed the corresponding interface in a mock
  and implement each method the test calls. On a client connection, `EngineConn`
  and `Request` return nil, `Handshake` returns its zero value, `Use` registers
  nothing, and the emitters from `To` or `Broadcast` return `ErrServerOnly` from
  `Emit` and `Send`. Client room methods maintain a local index; they do not join
  server rooms. `Send`, `Disconnect`, `Connected`, `Volatile` and `Compress`
  have client behavior. Server and client connection types now belong to their
  respective packages, with role-specific callbacks and errors.
- engineio: the reference `cookie` default now sets an `io` session cookie on
  every response from the polling transport that opened the session, where no
  cookie was set before. Defaults are Path=/, HttpOnly and SameSite=Strict;
  websocket-first connections set none. Configure `WithCookie` to change
  attributes or `WithCookieEnabled(false)` to opt out.
- engineio: the reference `httpCompression` default now compresses polling
  responses of at least 1024 encoded bytes with gzip or deflate when accepted
  by the client. Earlier responses were uncompressed. Set
  `WithHTTPCompression(false)` to opt out or `WithCompressionThreshold` to change the
  boundary. Websocket permessage-deflate remains opt-in.
- engineio: CORS defaults now send polling access-control headers and accept
  cross-origin websocket upgrades. Set `WithCORS(false)` for the former
  polling behavior and websocket same-origin checks; explicit transport
  `CheckOrigin` callbacks still take precedence.

- socketio: middleware and `OnConnect` rejection send an ERROR packet and keep the
  engine connection open. The rejected namespace joins no ID room and receives no
  `OnDisconnect`; packets emitted during the rejected connect are discarded.
- socketio: `Conn.Close` on a server writes DISCONNECT per connected namespace and
  calls `OnDisconnect` with `server namespace disconnect`. Reference clients report
  `io server disconnect` and do not reconnect. Earlier queued packets arrive first;
  these control packets retain space when the bounded application queue is full.
- socketio: `Conn.ID()` outside the root namespace is now `<namespace>#<engine id>`,
  as the reference's `id`; the socket joins the room with that ID after acceptance.

- socketio: OnDisconnect now reports `transport close`, `ping timeout`,
  `transport error` or `forced close` for connection closure instead of always
  `client namespace disconnect`. The `disconnecting` event runs while rooms are
  joined; OnDisconnect runs after they have been left on every disconnect path.

- engineio: `PingInterval` defaults to 25 seconds and `PingTimeout` to 20 seconds,
  as in engine.io 3.6.2. Every received packet renews a window of their sum, so an
  active client stays connected and a silent client times out after that window.
- engineio: a polling POST after `3probe` and before the client's UPGRADE packet is
  accepted and delivered, where it previously returned HTTP 400. Polling is paused
  only after UPGRADE; pending idle polls get a NOOP at 100 ms checks during the probe.
- engineio: invalid handshakes return JSON error codes 0–4 with HTTP 400 or 403;
  `RequestChecker` runs on handshakes and returns a fixed `Forbidden` message on
  rejection. A new session requires GET, so OPTIONS without a sid creates none.

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
- Engine.IO server sessions receive their logger from `engineio.WithLogger`.
- `logger.Error` accepts a nil error instead of panicking.
- Toolchain: `go 1.22` in `go.mod`; `golang.org/x/exp/slog` replaced by `log/slog`;
  `gofrs/uuid` replaced by `google/uuid`; `gorilla/websocket` 1.5.3; `testify` 1.12.1;
  `io/ioutil` replaced by `io` (roadmap stage 1.1).
- Lint: `.golangci.yml` migrated to the golangci-lint v2 schema with the standard
  linter set. Empty client addresses return `client.ErrEmptyAddr`.
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
  The role-specific options and package-level fallback described above route library
  records. The `logger` package godoc documents the routing (roadmap task 1.D).
- Backpressure: `Emit` never blocks; it blocked until the writer took the packet
  (`connection.go:136` at `48cf0d2`). A connection whose queue overflows is closed
  without draining and reports `ErrWriteBufferFull`; more than `WriteBufferSize` packets
  queued faster than they are written can close a healthy client, polling ones much
  sooner. See [`Namespace.Emit`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io#Namespace)
  (roadmap task 1.B).
- Server `Conn.Close` and client `Client.Close` drain: they run `OnDisconnect` and return,
  the packets queued until then are written in the background, and the engine.io
  connection closes when they are written or after the role's `WithCloseTimeout` duration
  (one minute if not positive); `Server.Count` counts the session until then. They closed
  engine.io at once (`connection.go:66` at `48cf0d2`).
  Closes started by the library (fatal read, decode or dispatch error, peer close, ping
  timeout or overflow) discard the queue. Encode errors and rejected namespace connects
  keep the engine connection open, as described above. See
  [`Conn`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io#Conn) (roadmap tasks 1.B
  and 1I).
- Except for queue overflow, an error that closes a connection is reported to
  `OnError` before the close's effects run.
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
  failures, returned errors other than the dialer's OPEN reader `Close` errors
  (including the polling client's stored
  request failures and the engine.io client's reader `Close` failures, which its next
  `NextReader` returns), the engine.io client's ping failures that are expected closure
  (its `Close` has started, `io.EOF`, a closed or lost connection, a websocket close
  frame from the peer, or a polling request failure after which the transport closed
  itself) and the websocket "frame not
  closed" reminders, which no longer carry a synthetic `ConnectionNotClosed` error;
  WARN for the engine.io client's other ping failures,
  the dialer's and the polling client's reader `Close` failures during the handshake,
  and `engineio: transport dial failed` (with
  `transport`) for a transport attempt whose error `Dial` does not return (the last
  attempt is DEBUG). The dialer's OPEN reader `Close` failures use WARN even
  when returned by `Dial`; they do not replace an earlier packet or decode error
  (`engineio/session/session.go:56`-`:507`, `engineio/client.go:70`-`:133`,
  `engineio/dialer.go:23`-`:87`, `engineio/transport/polling/connect.go:38`-`:277`,
  `engineio/packet/encoder.go:40`, `engineio/transport/websocket/wrapper.go:66`, `:131`
  at `1151bad`, roadmap task 1.L).
- Examples: the old `notice`/`msg`/`bye`/`echo` demo is replaced in every example by a port of the
  Socket.IO chat example (`socket.io-client` 2.5.0 page in `_examples/asset/`, server logic in `chat.go`).

### Deprecated

- `logger.Error` and `logger.Info`: the library no longer calls them. Use `slog`'s
  methods on `logger.Log` or on the logger configured for that server/client role. See
  [`logger`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io/logger), whose
  godoc now documents the levels, the message pattern and the attribute keys of library
  records (roadmap task 1.L).

### Removed

- Public Socket.IO server/client and Engine.IO server option bags; constructor
  configuration now uses the corresponding `With…` functions.

- Root client wrappers and type/error aliases, client `NewClient`, the server-package
  Engine.IO dialer and the public `engineio/session` layer. See
  [the v1 migration guide](docs/V1_MIGRATION.md) for role-specific replacements.

- engineio: JSONP polling. The `j` query parameter is ignored: a poll answers the ordinary
  payload with the usual headers instead of `___eio[<j>]("...")`, in which `j` was copied
  from the query unvalidated. A JSONP form POST (`d=`) is rejected with HTTP 400, as it was
  before. Clients must use XHR polling or websocket (roadmap Stage V1, decision D4).

### Known limitations

- Redis broadcast: `Emit` and `Send` from socket-excluding builders, including
  `Conn.To` and `Conn.Broadcast`, return `ErrExceptUnsupported` before delivery.
  Socket ID snapshots describe this instance only. `Volatile` and `Compress(false)` are honored locally;
  the legacy Redis wire format omits these flags, so remote instances use
  ordinary delivery and default compression. A room union publishes per room
  and can deliver duplicates across instances. Mixed Go/Node Redis clusters
  are unsupported; see [protocol deviation D3](docs/PROTOCOL.md#deviation-d3-mixed-gonode-redis-cluster).
- Redis broadcast: `Server.Adapter` does not honor `RedisAdapterOptions.DB` and
  uses DB 0. Binary arguments across instances have not been validated.
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
