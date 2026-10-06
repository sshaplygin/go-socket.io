# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow SemVer.

## Unreleased

### Fixed

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
  open until `pingTimeout`. This also removes a 60 s wait in `go test ./engineio`.
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
  [`engineio.Server.Close`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io@master/engineio#Server.Close)
  (`engineio/server.go:53`, `:178` at `79a393c`, roadmap task 1I).
- A namespace whose Redis broadcast could not be created held a nil broadcast, so every
  room call on it panicked, and the error was dropped. See
  [`Server.Adapter`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io@master#Server.Adapter)
  and [`Server.Serve`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io@master#Server.Serve)
  (`namespace_handler.go:27` at `79a393c`, roadmap task 1I).
- Concurrent handler registrations on one new namespace could each build a Redis
  broadcast and replace each other's handler (`server.go:346` at `79a393c`, roadmap
  task 1I).
- Building a namespace's Redis broadcast waited without a limit for a server that
  accepted the connection and never answered AUTH or SELECT, which with 1I also kept
  `Server.Close` waiting; both connections must now be dialled, AUTH and SELECT
  included, within 10 s (`redis_broadcast.go:119` at `79a393c`, roadmap task 1I).
- `Server.Close` left the Redis connections and subscriber goroutine of every namespace
  running. See [`Server.Close`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io@master#Server.Close)
  (`server.go:69` at `79a393c`, roadmap task 1I).

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
- `engineio.Options.WriteBufferSize` (temporary v1 placement) and `ErrWriteBufferFull`:
  each connection queues at most that many outbound packets (default 64). See
  [`engineio.Options`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io@master/engineio#Options)
  and [`ErrWriteBufferFull`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io@master#ErrWriteBufferFull)
  (roadmap tasks 1.B and 1I).

### Changed

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
- Backpressure: `Emit` never blocks; it blocked until the writer took the packet
  (`connection.go:136` at `48cf0d2`). A connection whose queue overflows is closed
  without draining and reports `ErrWriteBufferFull`; more than `WriteBufferSize` packets
  queued faster than they are written can close a healthy client, polling ones much
  sooner. See [`Namespace.Emit`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io@master#Namespace)
  (roadmap task 1.B).
- `Conn.Close` and `Client.Close` drain: they run `OnDisconnect` and return, the packets
  queued until then are written in the background, and the engine.io connection closes
  when they are written or after `engineio.Options.PingTimeout`; `Server.Count` counts the
  session until then. They closed engine.io at once (`connection.go:66` at `48cf0d2`).
  Closes started by the library (read, decode, dispatch or encode error, peer close, ping
  timeout, overflow, failed connect) discard the queue. See
  [`Conn`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io@master#Conn) (roadmap tasks 1.B
  and 1I).
- An encode error now closes the connection; it used to leave it open
  (`server.go:309` at `48cf0d2`).
- The error that closes a connection is reported to `OnError` before the close's effects
  run; a failed connect was closed before it was reported (`server.go:249` at `48cf0d2`).
- The warning for an invalid `SOCKETIO_LOG_LEVEL` is logged as
  `logger: invalid level ignored` with the value under `value`; it read
  `logger: invalid SOCKETIO_LOG_LEVEL, ignored` (`logger/logger.go:67` at `1151bad`,
  roadmap task 1.L).

## v1.4.2 and earlier

See the upstream release notes at
<https://github.com/googollee/go-socket.io/releases>.
