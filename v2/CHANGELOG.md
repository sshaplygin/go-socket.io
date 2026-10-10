# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow SemVer.

This file records the v2 module, `github.com/sshaplygin/go-socket.io/v2`. The v1 module at
the repository root has its own [CHANGELOG.md](../CHANGELOG.md).

## Unreleased

### Added

- `adapter/codec`: encoder and decoder for the messages of the non-sharded Node Redis adapter
  (`@socket.io/redis-adapter@8.3.0`): the MessagePack broadcast `[uid, packet, opts]`, the JSON
  requests (all-rooms, join, leave, disconnect, fetch-sockets, server-side emit) and the
  responses that list rooms, socket ids or socket snapshots, over wire types local to the
  package. Decoders are bounded (message size, nesting depth, binary values) and have fuzz
  targets; the 22 publications captured from Node and the pinned Node oracle are in
  `adapter/codec/testdata`, and the supported ones re-encode to Node's exact bytes. It adds
  `github.com/vmihailenco/msgpack/v5` to `v2/go.mod`. Nothing uses the package yet
  (the memory adapter and the broker adapters come later), so the library behaviour is
  unchanged.
- `_experiments/adapter-rooms`: standalone module (not imported by the v2 module) with
  22 fixtures captured from the Node in-memory adapter (`socket.io-adapter` 2.5.5) for
  room membership and recipient selection, a Go fixture validator and a Node script that
  reproduces the fixtures; preparation for the stage 2.2 memory adapter, no change to the
  library.
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
- `parser`: the Socket.IO protocol 5 wire codec (roadmap 2.3P), built on the frozen
  `Packet`, `Arguments`, `BinaryValue` and `ArgumentCodec` types. `Encode` and `Decode` convert a
  `Packet` to and from a complete message (text envelope plus ordered binary frames);
  `Assembler` accepts the frames of a connection one at a time and exposes the attachment
  deadline. Both are bounded by `Limits` (`MaxEventBytes` 1 MiB, `MaxAttachments` 64,
  `MaxDepth` 64, `AttachmentTimeout` 10 s; zero selects the default, a negative value is
  `ErrLimit`), validate namespaces, acknowledgement IDs up to 2^53-1, JSON payload shapes,
  reserved event names and binary placeholders, copy every buffer they keep and never
  modify their input. `EventPacket`, `AckPacket`, `EventArguments`, `AckArguments`,
  `Arguments.Validate`, `Concat` and `Arguments.Slice` build and split packets and join the
  arguments of a multi-argument codec; `JSON[T]` is an `ArgumentCodec` that sends a `T` as one
  argument and turns a `BinaryValue` nested in a struct, slice, map, pointer or interface
  into an attachment. Error sentinels: `ErrInvalid`, `ErrLimit`, `ErrTooLarge`,
  `ErrAttachments`, `ErrTooManyAttachments`, `ErrDepth`, `ErrArity`, `ErrUnsupported`,
  `ErrUnexpectedFrame`, `ErrAttachmentTimeout`. The codec comes from the
  `_experiments/sio5-codec` module, which this change deletes together with its tests,
  fuzz targets and Node oracle (now `parser/testdata/oracle`, pinned to
  `socket.io-parser` 4.2.7). Nothing in the root package calls it yet.
- `engineio.BenchmarkIdleConnections` (`engineio/idle_bench_test.go`): opens N idle
  websocket sessions against an `engineio.Server` running in a subprocess and reports the
  server's RSS and goroutines. N is 200 by default and `IDLE_CONNS=10000` selects the
  roadmap figure. It uses only `engineio.Server`, `client.Dialer` and `websocket.Default`,
  so the same file measures the `gobwas/ws` transport; it skips outside linux and darwin.
  Test code only, no change to the library.

  Baseline for roadmap 2.1, BEFORE the `gobwas/ws` swap (Engine.IO v3 server on
  `gorilla/websocket` v1.5.3, the library code of `cb0dd90`; the benchmark commits
  add test files only, so the measured library code is identical). Apple M1 Max (10 cores, 32 GiB),
  macOS 26.2 (Darwin 25.2.0), Go 1.25.5 darwin/arm64, server and clients on loopback on the
  same machine. Three separate runs of

  ```sh
  IDLE_CONNS=10000 go test -count=1 -run '^$' -bench BenchmarkIdleConnections -benchtime=1x ./engineio/
  ```

  gave, per run, server RSS after 10000 sessions of 284.1, 285.2 and 286.2 MiB
  (`rss-total-MiB`; 13.1-13.3 MiB before the first session, so 28449, 28554 and
  28656 B per session), 20006 server goroutines (6 before, 2.00 per session) and a
  connect phase of 480, 531 and 539 ms (48.0, 53.1 and 53.9 us per session with 32
  parallel dialers). The default, `go test -run '^$' -bench BenchmarkIdleConnections
  -benchmem -count=5 ./engineio/` (N=200; its numbers are in the `--- BENCH` log line, because
  the benchmark workflow's report tool accepts only the standard metric units), gave 20.8-21.3 MiB RSS (40305-42844 B per
  session, higher per session than at 10000 because one-time warm-up of a cold server is
  counted), 406 server goroutines (2.00 per
  session) and 1.24-1.28 s per run, which includes a one-second idle hold. These are one
  machine and one set of runs, advisory, not a performance claim; a run is shorter than the
  default 20 s ping interval, so heartbeat cost is not measured, and the Linux path was run
  only on the CI runner, its figures are not recorded here; the AFTER numbers follow
  below. RSS is read with `ps -o rss=` after the
  server ran `debug.FreeOSMemory`. A goroutine dump of the server at 50 sessions shows the
  two goroutines per session: the `net/http` handler goroutine of the upgrade request,
  blocked in the websocket transport's `ServeHTTP`, and the benchmark's own read loop
  (one `NextReader` per accepted session, as `socketio.Server` starts per connection).

  AFTER the swap (`gobwas/ws` v1.4.0 transport, still the Engine.IO v3 handshake and
  heartbeat), the library code of `fc9220f`, same machine, OS and Go (1.25.5), same command.
  Four runs gave server RSS after 10000 sessions of 117.5, 115.5, 117.6 and 102.5 MiB (13.1-13.4
  MiB before the first session; 10948, 10735, 10959 and 9391 B per session) and 10006 server
  goroutines (6 before, 1.00 per session). The default (N=200) with `-benchmem -count=5` gave
  18.9-19.5 MiB RSS (31048-33833 B per session), 206 goroutines (1.00 per session) and
  1.48-1.97 s per run. The machine was not quiet: the load average was 21-39 (another
  application used about 3.6 cores), so the connect phase (5.1-17.8 s here, against 0.5 s in
  the BEFORE runs) is not comparable. To compare under that load, `ed94997` (gorilla,
  the BEFORE library code) was run right before and after on the same machine: 270.8 and 234.6
  MiB, 20006 goroutines, connect phase 15.8 and 7.0 s. Where the saving comes from: with the
  websocket connection's `ServeHTTP` blocked until close, as it was, the same transport gave
  277.5 and 280.8 MiB with 20006 goroutines, no change from the BEFORE figures. The 2.4 to
  2.7 times lower RSS and the halved goroutine count come from `ServeHTTP` returning at once
  after the hijack: the request goroutine and the `net/http` connection state it kept are
  released, and the transport keeps no read buffer of its own (`ReadBufferSize` unset reads
  the socket directly). One machine, advisory, not a performance claim; the benchmark does not
  measure throughput, and a run is shorter than the ping interval.

### Changed

- The module path is `github.com/sshaplygin/go-socket.io/v2`, and the module lives in `v2/`
  of the repository (repository restructure, step B). Imports of the v2 packages change from
  `github.com/sshaplygin/go-socket.io/<pkg>` to `github.com/sshaplygin/go-socket.io/v2/<pkg>`.
  The repository root is the v1 module again: `go get github.com/sshaplygin/go-socket.io@master`
  now resolves to v1, and the v2 module is
  `go get github.com/sshaplygin/go-socket.io/v2@master`.
- The WebSocket transport is rewritten on `github.com/gobwas/ws` v1.4.0 (stage 2.1);
  `gorilla/websocket` leaves `go.mod`. The server upgrades with `ws.UpgradeHTTP` (HTTP/1.1
  hijack only); the client dials with `ws.Dialer`. Each Engine.IO packet is one WebSocket
  message, coded by the prepared v4 codec, now in package `websocket` (`Encode`, `Decode`,
  `Packet`, `ErrTooLarge`, `ErrInvalidPacket`, `ErrInvalidLimit`): a binary message is the raw
  data of a MESSAGE packet, without the type byte of v3, and `b` + base64 text is read as binary.
  The handshake, heartbeat and `EIO` check are still Engine.IO v3, so a v3 peer that sends binary
  over websocket no longer interoperates. A message is limited to `websocket.Transport.MaxPayload`
  (new field, default 1 MiB as for polling), fragments together; the transport answered any
  size before. A peer that violates the protocol is sent a close frame with status 1002, 1007
  (invalid UTF-8) or 1009 (too large), then a TCP half-close and up to one second of draining
  before the socket is closed, so the frame is not lost to a reset; a `Close` by the consumer during that window fails further reads and writes at once and leaves the socket close to the drain. A response writer that is not an
  `http.Hijacker` is answered with HTTP 501 and the log line `engineio: request rejected`
  with `reason="no hijacker"`; a rejected handshake is reported as `websocket.HandshakeError`
  (it replaces the gorilla `HandshakeError` check of `engineio.Server`). `websocket.DialError`
  gains `Unwrap`. The connection's `ServeHTTP` returns at once instead of blocking until close.
  Unchanged: `CheckOrigin` (nil means same origin), `ReadBufferSize` and `WriteBufferSize`,
  `HandshakeTimeout`, `TLSClientConfig`, `Subprotocols`, `NetDial` and `Proxy`. `Proxy` supports
  `http` proxies through CONNECT; any other scheme fails the dial (gorilla also
  accepted SOCKS5 proxies). `ReadBufferSize` zero now reads the socket unbuffered, where gorilla
  allocated 4 KiB per connection. `permessage-deflate` is not supported (the gorilla
  transport never enabled it either). `x/sys` v0.6.0 enters the module graph through `gobwas/ws`.
  The standalone `_experiments/eio4-websocket` (the framing prototype, its Go tests and its Node
  `ws@8.18.3` peer, now `engineio/transport/websocket/testdata/reference` with `TestNodeOracle`)
  and `_experiments/ws-bench` are deleted, with their Unreleased entries.
- CI: `make examples` only checks that every `_examples/*/chat.go` is identical, and
  `make vuln` no longer scans the `_examples` modules: the legacy examples build against
  the removed v1 runtime until stage 2.5D migrates them. The `lint` job runs
  `make graph`.
- CI: `make experiments` vets, format-checks, lints and race-tests every standalone
  `_experiments/*/go.mod` module in a new `experiments` job; `make vuln` and Dependabot
  (`gomod`, weekly) cover those modules too.
- CI: `make experiments` first checks that `_experiments` stays standalone (no import
  of the v2 module, no `go.work`, every Go file under a `go.mod` of its own).
- `engineio/payload` and the polling transport use the Engine.IO v4 polling payload
  (stage 2.1): records separated by `0x1e`, binary packets as `b` + base64, always
  `text/plain; charset=UTF-8`. The prepared codec moved from `engineio/payload/internal/eio4`
  into package `payload` (`Encode`, `Decode`, `DecodeReader`, `EncodeBatch`, `Packet`,
  `ErrTooLarge`, `ErrInvalidPayload`, `ErrInvalidLimit`) with its fixtures, fuzz tests and
  pinned Node oracles. A POST body is read up to `polling.Transport.MaxPayload` (default
  1 MiB) before decoding: 413 when larger, 400 when malformed. A poll response and a client
  POST carry every packet that the session writers hand over at once, a POST up to the
  server's `maxPayload`, or to the client's `MaxPayload` (default 1 MiB) until it is advertised. A client response larger than its `MaxPayload` ends the session with `payload.ErrTooLarge`. Closing a polling client aborts its pending requests.
  `transport.ConnParameters` gains `MaxPayload` (JSON `maxPayload`, omitted when zero).
  The handshake, heartbeat and `EIO` check are still Engine.IO v3 until the rest of 2.1
  lands, so a v3 peer no longer interoperates over polling.
- `payload.New` takes the read and write limits instead of a binary flag, and `FeedIn`
  no longer takes one.

### Removed

- `parser`: the Socket.IO v4 packet codec of the v1 runtime (`Header`, `Payload`, `Encoder`,
  `Decoder`, `FrameReader`, `FrameWriter`, `Buffer`, `BufferData` and `ErrInvalidPacketType`).
  Nothing in the module used it after stage 2.0; it stays in the v1 module at the repository root. `parser.Type`
  keeps its name and its values 0 to 4, now means the base type only (a binary event is an
  `Event` with attachments), and the constant `Error` is renamed `ConnectError`.
- Polling JSONP (the `j` parameter, removed from Engine.IO v4), the `b64` parameter, the
  `application/octet-stream` polling body and the v3 length-prefixed payload encoder and
  decoder. The Engine.IO v3 polling framing is gone from the v2 module; it stays in the v1 module at
  the repository root.
- The v1 root runtime: the reflection-based `Server`, `Client`, namespace and handler
  API, the memory and Redis broadcast, `Server.Adapter` and the `redigo` dependency (and
  the test-only `miniredis` and the `uuid` dependency with it). The v1 code stays in the v1
  module at the repository root.
