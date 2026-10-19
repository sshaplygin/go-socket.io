# Parity matrix: Node reference vs Go v1

This file owns the parity matrix of the v1 line (the repository-root module) against the
Node.js reference: for each reference feature the status, the evidence, how it was verified,
and the plan decided for it. The order of the work, the PR scope and the release gates are in
[ROADMAP.md](ROADMAP.md), Stage V1; the protocol facts and the documented deviations are in
[PROTOCOL.md](PROTOCOL.md). The v2 line has its own reference (socket.io 4.x, engine.io 6.x)
and is not covered here.

## Basis of the audit

Compared revision: `origin/v1.x` = `89958005f2ac884d0bb6bc4d50b5cd13f4270378` (the tree that the
repository restructure restores at the repository root). It was read from an archive copy;
one throwaway test file was added to that copy and deleted, the repository was not touched.
Go paths in the tables are relative to that tree, so they are repository-root paths once the
restructure has merged.

Reference (installed from npm, source read): `socket.io` 2.5.0, `engine.io` 3.6.2,
`socket.io-adapter` 1.1.2, `socket.io-parser` 3.4.5, `engine.io-parser` 2.2.1; client side
`socket.io-client` 2.5.0, `engine.io-client` 3.5.6. Adapter-level extras (`remoteJoin` etc.)
exist only in `socket.io-redis` 5.4.0, whose installed sources were read too. A `Ref` path is a
file in the installed socket.io 2.5.0 / engine.io 3.6.2 sources (`socket.io/lib/...`,
`engine.io/lib/...`, a bare `adapter` or `redis` prefix means the `socket.io-adapter` 1.1.2 or
`socket.io-redis` 5.4.0 package, and `client lib/...` the `socket.io-client` 2.5.0 package).

## Reading the tables

- Status: `PARITY` (same observable behaviour, API shape may differ: D9), `PARTIAL`, `ABSENT`,
  `N/A` (JS-only or not part of the reference). Status, evidence and `Ver` are the audit result
  at the compared revision; the PR that closes a row updates them in the same change.
- Basis: every row comes from the **installed source** ("implemented"). The socket.io 2.5.0
  `Readme.md` is a stub with no API reference, so socket.io 2.x method docs are **not
  available offline**; the only documented-API source is `engine.io/README.md` (rows marked
  **D** also appear there). Rows marked **[not in ref]** were in the task list but do not
  exist in the installed reference version.
- Ver: `R` = read in code, `T` = observed by running a probe/test, `U` = could not verify
  (stated as a hypothesis).
- API? = closing the row needs a NEW PUBLIC Go API (`Y`), only behaviour/internal change
  (`N`), or `-`. PRs = the audit's rough PR estimate, 0 for PARITY; the plan is in `Plan`.
- Adding methods to the exported `Conn` / `Namespace` interfaces breaks external implementers
  (mocks); decision D10 (ROADMAP Stage V1) adds them directly, and `v1.5.0` documents the
  change; the plan applies the same rule to the exported `adapter.Adapter` and to `engineio.Conn`
  (E20, D16). `D10` in `Decision` marks the rows
  whose plan puts a method on `Conn`, `Namespace` (the per-connection view embedded in `Conn`),
  `adapter.Adapter` or `engineio.Conn`. The V1-0 contracts are the list of those methods: a contract that puts one on
  an untagged row adds `D10` to it in the same PR. Untagged on purpose: the server namespace
  object of S8 (S14, S16, N2, N4), the ack parameter of a handler (K26), handler registration on
  the server (K28), the flags of server broadcasts (S21: `Server` is a struct; the flags reach
  `adapter.Adapter` through the methods of A4).
- `Plan` is `<kind> V1-<n>` with the PR of ROADMAP Stage V1 that closes the row, several
  entries separated by `;`. Kinds: `BUG` fixed as a bug (a behaviour change); `ADD` new public
  API; `CHG` behaviour or internal change without new API; `TEST` a test verifies an unverified
  claim and the row changes only if the test disagrees; `DROP` declared unsupported and
  removed; `DEV` documented deviation, not closed in v1; `REDIS` the Redis follow-up stage;
  `-` nothing to do. The PR that closes a row rewrites the
  kind to `DONE`, keeps its number, and adds the comment `// Covers <ID>` directly above the test function that closes it (the Stage V1
  DoD finds the function with `go test -list`).
- `Decision` names the owner decision behind the plan: `D1` to `D19`, all settled, in ROADMAP
  Stage V1, which owns their text; no decision is pending.
- ID: one capital letter and a number (the shape the parity check reads). Sections 1 to 8
  use `S`, `N`, `K`, `A`, `E`, `P`, `R` and `C`; `B` rows are defects the owner reported
  after the audit; `G` rows are the findings of the review of 2026-10-18 at `3db9751`
  (*Findings of the review of 2026-10-18*), closed in wave V1G of ROADMAP Stage V1; G30–G31 record the additional frame-release findings; `T` rows
  are the typed API that D18 requires. A row keeps its ID for good; a new row takes the next
  free number of its series, and a defect found twice is one row.

## 1. Server (`socket.io/lib/index.js`)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| S1 | - | - | `new Server(srv, opts)` | index.js:43 | PARITY | `NewServer(...Option)` in server.go | `NewServer` | - | 0 | R |
| S2 | - | - | `path` option / `path()` | index.js:210 | PARITY | Server is an `http.Handler` (server.go:117); path is the mux pattern | mux registration | - | 0 | R |
| S3 | - | - | `serveClient` (serves `/socket.io.js`) | index.js:105,335 | N/A | JS client bundle serving; examples serve static files with `http.FileServer` | - | - | 0 | R |
| S4 | DONE V1-18 | D15 | `origins` option / `origins()` (default `*:*`, list or `fn(origin, cb)`) | index.js:69-95,243 | PARITY | `TestCORSDisabled` verifies no polling CORS headers and same-origin websocket checks with DisableCORS; explicit transport CheckOrigin overrides either setting. The review of 2026-10-18 listed the permissive default as known: it is D15, not a defect. | `engineio.WithCORS(false)`, transport `CheckOrigin` | Y (the option that disables the default) | 1 | T |
| S5 | DONE V1-7 | D5 | `adapter(v)` pluggable adapter class | index.js:224 | PARITY | `TestSetAdapter` verifies factory construction once, Join/SendTo/IDs calls, and errors surfaced by Serve. `TestNamespaceAdapter` checks WithAdapter option order and last SetAdapter wins for later namespaces. | `SetAdapter`, `adapter.Factory` | Y | 2 | T |
| S6 | - | - | `parser` option (custom parser) | index.js:54 | N/A | fixed parser package; custom parsers not a v1 goal (msgpack is a v2 roadmap item) | - | - | 0 | R |
| S7 | - | - | `attach` / `listen(srv or port)` | index.js:259 | PARITY | `ServeHTTP` + `Serve()` server.go:117,172 (idiomatic Go; `listen(port)` convenience not needed) | `Server.ServeHTTP`, `Serve` | - | 0 | R |
| S8 | DONE V1-5 | D5 | `of(name[, fn])` static namespace, returns `Namespace` | index.js:453-476 | PARITY | Server.Of normalizes names and shares the stable handler and namespace object. Verified by `TestServerOf`. | `Server.Of` | Y (`Of`) | 2 | T |
| S9 | DONE V1-7 | D5 | `of(regex)` / `of(fn)` dynamic namespaces, `ParentNamespace`, `checkNamespace` | index.js:454-464,174-200; parent-namespace.js | PARITY | `TestDynamicNamespaces` verifies ordered matching and query isolation, child reuse, inherited middleware/listeners, independent Once copies, parent broadcasts/snapshots and rejection without closing root. | `OfFunc`, `ServerNamespace` | Y | 2-3 | T |
| S10 | DONE V1-4 | D8 | CONNECT to unknown namespace answers `ERROR "Invalid namespace"`, connection stays open | client.js:69-76 | PARITY | unknown CONNECT replies with ERROR and a following root ACK succeeds. Verified by `TestUnknownNamespaceKeepsConnection`. | - | N | 1 | T |
| S11 | DONE V1-5 | D5 | `use(fn)` namespace middleware `fn(socket, next)`, error becomes `ERROR` packet | index.js:509; namespace.js:102,119,172-176 | PARITY | namespace middleware and OnConnect rejection send ERROR and retain the engine/root socket. Verified by `TestNamespaceMiddleware`. | `Server.Use; ServerNamespace.Use` | Y | 2 | T |
| S12 | DONE V1-3 | D8 | `emit(ev, ...)` to all sockets of `/` (once per socket) | namespace.js:217; adapter 122-163 | PARITY | `TestNamespaceBroadcastOnce`: two sockets in several rooms receive a single namespace broadcast followed by a distinct marker; `TestLifecycleRootNamespace` also asserts no duplicate | `Server.BroadcastToNamespace` | N | 1 | T |
| S13 | DONE V1-6; REDIS | D1, D2 | `to(room)/in(room).emit` (one room, or union of several rooms with dedupe) | namespace.js:147; adapter 122 | PARTIAL (Redis) | `ServerNamespace.To` and memory `SendTo` dedupe room unions; `TestRoomUnion` verifies immutable builders and one delivery. Redis dedupes locally and publishes per room. | `ServerNamespace.To`, `adapter.Adapter.SendTo` | N | 1 | T |
| S14 | DONE V1-5 | D5 | `send` / `write` (emits `message`) | namespace.js:255 | PARITY | namespace Send delivers the message event. Verified by `TestNamespaceSend`. | `ServerNamespace.Send` | Y (sugar) | 0-1 | T |
| S15 | DONE V1-6; REDIS | D5, D2 | `clients(cb)` list of socket ids (all, or `in(room)`) | namespace.js:270; adapter 173 | PARTIAL (Redis) | `TestNamespaceSocketIDs` checks all/room snapshots and copies. Redis IDs list this instance only (`TestRedisBroadcastLocalFlag`). | `ServerNamespace.SocketIDs` | Y | 1 | T |
| S16 | DONE V1-5 | D5 | `nsp.connected`, `nsp.sockets` maps | namespace.js:55-56 | PARITY | connected sockets are snapshotted by ID; disconnected and rejected sockets are absent. Verified by `TestNamespaceSockets`. | `ServerNamespace.Sockets; Socket` | Y | 1 | T |
| S17 | DONE V1-5 | D8 | `close(fn)` closes all clients + http server | index.js:485-499 | PARITY | Server.Close waits for OnDisconnect after closing sessions; a bounded wait is covered by TestServerCloseBound. Verified by `TestServerCloseWaits`. | `Server.Close` | N | 1-2 | T |
| S18 | DONE V1-16 | D16 | `engine` / `eio` property | index.js:425 | PARITY | `TestServerEngine`: stable, non-nil handle of the engine created by NewServer | `Server.Engine` | Y | 1 | T |
| S19 | - | - | `set(key,val)` legacy BC, `bind`, `onconnection`, `httpServer`, `checkRequest` | index.js:143,424-443 | N/A | deprecated or JS plumbing | - | - | 0 | R |
| S20 | - | - | event `connection` / `connect` | namespace.js:189-190 | PARITY | `OnConnect(ns, func(Conn) error)` server.go (`OnConnect`); one handler per namespace, a second call replaces it (internal/namespace/handlers.go) | `OnConnect` | - | 0 | R |
| S21 | DONE V1-6; DONE V1-15; REDIS | D5, D11, D2 | flags `volatile`, `local`, `json`, `compress(b)`, `binary(b)` on server | index.js:509-523; namespace.js:32 | PARTIAL (Redis) | `TestServerBroadcastFlags` checks volatile/local; `TestBroadcastCompressFlag` checks immutable compression flags on namespace broadcasts and room targets. Redis honours compression locally and publishes without it (legacy wire format). | `ServerNamespace.Volatile`, `Local`, `Compress` | Y | see K | T |
| S22 | DEV | D19 | Admin UI: `instrument(io, opts)` of `@socket.io/admin-ui` **[not in ref: needs a socket.io >= 3.1 server and socket.io-client ^4.5]** | - | N/A | not in v1 (D19): the official Admin UI speaks Socket.IO protocol v5 over Engine.IO v4, which the v1 module does not; it is v2 Stage 5 (`v2/contrib/admin`). Documented in `PROTOCOL.md`, Deviation D19 | - | - | 0 | R |

## 2. Namespace (`socket.io/lib/namespace.js`)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| N1 | - | - | `name` | namespace.js:53 | PARITY | `Conn.Namespace()` internal/connection/socket.go | `Conn.Namespace` | - | 0 | R |
| N2 | DONE V1-5 | D5 | `use` | namespace.js:102 | PARITY | middleware runs in registration order before OnConnect and the ID room join. Verified by `TestNamespaceMiddleware`. | `ServerNamespace.Use` | Y | see S11 | T |
| N3 | DONE V1-6 | D1 | `to/in/emit/send/clients` per namespace | namespace.js:147-279 | PARITY | `TestServerNamespaceMethods` drives /chat To/Emit/Send/SocketIDs and legacy broadcasts over real polling sockets. | `ServerNamespace` | - | see S12-S15 | T |
| N4 | DONE V1-5 | D5 | `connected`, `sockets`, `ids`, `rooms` | namespace.js:55-59 | PARITY | namespace snapshots and ID lookup track connected sockets independently of their rooms. Verified by `TestNamespaceSockets`. | `ServerNamespace.Sockets; Socket` | Y | see S16 | T |
| N5 | DONE V1-7 | D5 | `adapter` handle (`nsp.adapter.rooms/sids`) | namespace.js:91 | PARITY | `TestNamespaceAdapter` verifies stable handles, Redis and injected factories, existing namespace preservation, nil for failed construction; dynamic parents return nil. | `ServerNamespace.Adapter` | Y | see S5 | T |
| N6 | - | - | events `connect`, `connection` | namespace.js:189 | PARITY | see S20 | `OnConnect` | - | 0 | R |
| N7 | DONE V1-6 | D1 | `emit` with callback throws "Callbacks are not supported when broadcasting" | namespace.js:229 | PARTIAL (legacy deviation) | `TestBroadcastCallback` verifies ErrBroadcastAck with nothing sent by new paths; all four legacy paths register and execute one ACK callback per socket. | `Emitter.Emit`, `ServerNamespace.Emit`, `Send` | N | 0-1 | T |
| N8 | DONE V1-7 | D5 | namespace CONNECT carrying a query (`/nsp?x=1`) available as `handshake.query` | client.js:208; socket.js:117-120 | PARITY | `TestNamespaceConnectQuery` observes CONNECT query in namespace middleware; protocol notes now name Handshake.Query. | `Conn.Handshake` | Y | 1 | T |
| N9 | DONE V1-4 | D1 | namespace CONNECT reply wire form `0/nsp,` | socket.js:311 | PARITY | namespace CONNECT replies with exactly 0/chat,. Verified by `TestNamespaceConnectPacket`. | - | N | 1 | T |
| N10 | - | - | root CONNECT merged into the handshake (`initialPacket`) | index.js:295-301; engine socket.js:72 | N/A | optimisation only; Go sends `0` right after OPEN (internal/connection/socket.go, `acceptPending`). Same client-visible result | - | - | 0 | R |

## 3. Socket (`socket.io/lib/socket.js`, `client.js`)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| K1 | DONE V1-5 | D15 | `id` (`/` : engine id, other nsp: `nsp#id`) | socket.js:64 | PARITY | root ID is the engine sid, other namespaces use nsp#sid and join that ID room. Verified by `TestConnIDNamespace`. | `Conn.ID` | N | 0-1 | T |
| K2 | - | - | `rooms` | socket.js:67 | PARITY | `Conn.Rooms()` internal/connection/socket.go; includes the sid room (lifecycle_test.go:75) | `Conn.Rooms` | - | 0 | R |
| K3 | - | - | `client` | socket.js:65 | N/A | no per-engine `Client` object in Go; conn-level state is internal | - | - | 0 | R |
| K4 | DONE V1-7 | D5, D10 | `conn` (engine socket: transport, readyState, upgraded) | socket.js:66 | PARITY | `TestConnEngineConn` reaches engine state/transport on a server socket and returns nil on a client socket. | `Conn.EngineConn` | Y | 1 | T |
| K5 | DONE V1-7 | D5, D10 | `request` (`http.IncomingMessage`) | socket.js:102 | PARITY | `TestConnRequest` checks original method/URL/header in root OnConnect and namespace middleware on 20 connections, retains requests after close, and checks nil for fakes/clients. | `Conn.Request` | Y | 1 | T |
| K6 | - | - | `handshake.headers` | socket.js:122 | PARITY | `Conn.RemoteHeader()` the embedded engine connection in internal/connection/connection.go | `RemoteHeader` | - | 0 | R |
| K7 | DONE V1-7 | D5, D10 | `handshake.query` (request query merged with nsp query) | socket.js:116-129 | PARITY | `TestHandshakeQuery` checks namespace query override, repeated values, root isolation and copied metadata. | `Conn.Handshake().Query` | Y | 1 | T |
| K8 | - | - | `handshake.address` | socket.js:124 | PARITY | `Conn.RemoteAddr()` | `RemoteAddr` | - | 0 | R |
| K9 | - | - | `handshake.url` | socket.js:128 | PARITY | `Conn.URL()` (request URL, no scheme/host) | `URL` | - | 0 | R |
| K10 | DONE V1-7 | D5, D10 | `handshake.time`, `issued`, `secure`, `xdomain` | socket.js:123-127 | PARITY | `TestHandshakeData` checks time, address, Origin and TLS inside root handlers and namespace middleware; a namespace created after websocket upgrade retains polling headers, URL and address. | `Conn.Handshake` | Y | 1 (with K7) | T |
| K11 | DONE V1-6 | D5, D10 | `join(room \| rooms[], cb)` | socket.js:236 | PARITY | `TestConnJoinRooms` verifies ordered variadic joins with duplicate rooms ignored. | `Join(rooms ...string)` | Y (variadic) | 1 | T |
| K12 | - | - | `leave(room, cb)`, `leaveAll()` | socket.js:269,287 | PARITY | `Leave`, `LeaveAll` internal/connection/socket.go | same | - | 0 | R |
| K13 | - | - | `emit(ev, ...args[, ack])` to this client | socket.js:140-181 | PARITY | `Conn.Emit` internal/connection/socket.go; trailing func is the ack callback (not sent), called on ACK | `Emit` | - | 0 | R |
| K14 | DONE V1-6 | D5, D10 | `socket.broadcast.emit` / `socket.to(room).emit` (everyone except sender) | socket.js:170-175,191 | PARTIAL (Redis) | `TestBroadcastExceptSender` executes handler broadcasts, volatile target conversion, single-socket Local, and client ErrServerOnly. Redis exclusion returns adapter.ErrExceptUnsupported before I/O. | `To`, `adapter.Adapter`, `Emitter.Except` | Y | 2 | T |
| K15 | DONE V1-5 | D5, D10 | `send` / `write` | socket.js:204 | PARITY | Send delivers message and its trailing acknowledgement callback. Verified by `TestConnSend`. | `Namespace.Send` | Y (sugar) | 0-1 | T |
| K16 | DONE V1-6 | D5, D10 | flag `volatile` (drop if transport not writable) | socket.js:39; client.js:167 | PARITY | `TestVolatileEmit` observes polling drops without a pending poll and websocket delivery; `TestVolatileDropsQueuedPacket` checks queue drops without ACK registration or close. | `Volatile` | Y | 2 | T |
| K17 | DONE V1-15 | D11, D10 | `compress(bool)` | socket.js:497 | PARITY | `TestEmitCompressFlag` reads raw RSV1 bits for pending emits, text/binary attachments, immutable builders and sender-excluding room broadcasts. | `Namespace.Compress`, `Emitter.Compress` | Y | 1 + E8 | T |
| K18 | - | - | `binary(bool)` | socket.js:510 | N/A | Go decides by `parser.Buffer` | - | - | 0 | R |
| K19 | - | - | flags `json`, `local` | socket.js:39-44 | N/A | `json` is a no-op; `local` only matters to a cluster adapter | - | - | 0 | R |
| K20 | DONE V1-5 | D5, D10 | `disconnect(close)`: DISCONNECT packet to the client for one namespace, or close the connection | socket.js:478-487 | PARITY | namespace Disconnect sends the packet and leaves other namespaces open; Close writes one packet per namespace. Verified by `TestConnDisconnect`. | `Namespace.Disconnect; Conn.Close` | Y (`Disconnect`) | 1-2 | T |
| K21 | - | - | event `error` | socket.js:429-436 | PARITY | `OnError(ns, func(Conn, error))` server.go:142 | `OnError` | - | 0 | R |
| K22 | DONE V1-4 | D1 | client `ERROR` packet becomes socket `error` | socket.js:345 | PARITY | string, object, array and null ERROR payloads reach OnError without closing. Verified by `TestClientErrorPacket`. | - | N | 0-1 | T |
| K23 | DONE V1-4 | D1 | event `disconnecting` (rooms still joined) | socket.js:449 | PARITY | disconnecting sees joined rooms; OnDisconnect sees no rooms on peer and application close. Verified by `TestDisconnectingRooms`. | `OnDisconnect` | Y | 1 | T |
| K24 | DONE V1-4 | D1 | event `disconnect` with reason (`client namespace disconnect`, `server namespace disconnect`, `transport close`, `transport error`, `ping timeout`, `forced close`) | socket.js:421,484 | PARITY | engine causes are wrapped and mapped to namespace reasons on the read goroutine. Verified by `TestOnDisconnectReason; engineio/internal/session.TestSessionCloseError`. | `OnDisconnect(Conn, string)` | N | 1-2 | T |
| K25 | DONE V1-5 | D5, D10 | `use(fn)` event middleware `fn(event, next)` | socket.js:548 | PARITY | typed and generic JSON/binary arguments reach event middleware; rejection skips the handler and ACK. Verified by `TestEventMiddleware`. | `Namespace.Use` | Y | 2 | T |
| K26 | DONE V1-7 | D5 | ack of client events: handler gets a callback `ack(...)`, may be called later, once only | socket.js:376-393 | PARITY | `TestLateAck` sends an ACK from another goroutine after a barrier proves handler return; repeated and unrequested ACKs send nothing. Mixed listeners suppress automatic return ACKs. The return-value path still writes an ACK without an id for an event that asked for none (G21). | `AckFunc` | Y | 1-2 | T |
| K27 | - | - | server to client ack callback | socket.js:153-161,401-410 | PARITY | `Emit(..., func(args))` ack stored per conn and dropped when called (internal/connection/socket.go, internal/connection/packets.go) | `Emit` | - | 0 | R |
| K28 | DONE V1-7 | D5 | many listeners per event, `on/once/off` | EventEmitter | PARITY | `TestEventListeners` checks order, Once, off, Off, replacement, type validation and snapshot changes; `TestOnceAcrossConnections` verifies one global invocation under concurrent clients. | `ServerNamespace.On`, `Once`, `Off` | Y | 1 | T |
| K29 | - | - | decode error / protocol violation: sockets get `error`, connection closed | client.js:191-198,228-235 | PARITY | `serveRead` reports to root `OnError` and returns (internal/connection/io.go) | `OnError` | - | 0 | R |

## 4. Rooms / Adapter (`socket.io-adapter` 1.1.2; Redis parts: `socket.io-redis` 5.4.0)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| A1 | DONE V1-6 | D1 | `rooms`, `sids` maps | adapter 21-26 | PARITY | `adapter.TestBroadcastRoomsMaps` checks both indexes after Join/Leave/LeaveAll/Clear, including sockets left in no rooms. Rooms and LeaveAll use sids. | internal | N | 0-1 | T |
| A2 | DONE V1-7 | D5 | `add/addAll(id, rooms, cb)` | adapter 43-65 | PARITY | `TestAdapterJoinRooms` observes ordered multi-room Join calls through an injected adapter and verifies membership/IDs. D9 keeps the Conn argument. | `Namespace.Join`, `SetAdapter` | Y | 1 | T |
| A3 | - | - | `del`, `delAll` | adapter 76-108 | PARITY | `adapter.Memory.Leave`, `LeaveAll` in adapter/memory.go | same | - | 0 | R |
| A4 | DONE V1-6; REDIS | D5, D2, D10 | `broadcast(packet, {rooms, except, flags})` once per socket | adapter 122-163 | PARTIAL (Redis) | `adapter.TestBroadcastSendTo` verifies union/dedupe, exclusion, flags through the minimal socket contract and lock-free delivery; root `TestBroadcastAdapterError` verifies error propagation. Redis exclusion fails before I/O; unknown message extensions are dropped and logged. | `adapter.Adapter.SendTo`, `adapter.Options` | Y (options) | 2 | T |
| A5 | DONE V1-6; REDIS | D5, D2, D10 | `clients(rooms, cb)` ids | adapter 173-211 | PARTIAL (Redis) | `adapter.TestBroadcastIDs` checks all/room unions, dedupe and copies. Redis IDs list this instance only. | `adapter.Adapter.IDs` | Y | 1 | T |
| A6 | - | - | `clientRooms(id, cb)` | adapter 220 | PARITY | `Conn.Rooms()`; `adapter.Adapter.Rooms(conn)` in adapter/memory.go | `Rooms` | - | 0 | R |
| A7 | - | - | `allRooms(cb)` **[not in socket.io-adapter 1.1.2; Redis 5.4.0 only]** | redis index.js:553 | PARITY | `Server.Rooms(ns)` server.go:299, `adapter.Adapter.AllRooms` | `Rooms` | - | 0 | R |
| A8 | DEV | D3 | `remoteJoin`, `remoteLeave`, `remoteDisconnect`, `customRequest` **[Redis 5.4.0 only, not core adapter]** | redis index.js:598,640,680,721 | ABSENT | none | - | Y | 2-3 (needs A9 first) | R |
| A9 | DEV | D3 | Redis wire interop with `socket.io-redis` 5.4.0 (msgpack, channels `prefix#nsp#room#`, `prefix-request#nsp#`, `prefix-response#nsp#`) | redis index.js:8,85-87 | ABSENT | Go uses JSON and channel `prefix#nsp#uid` (adapter/redis/adapter.go and codec.go): Go instances talk to each other only (not run against Node); the receiver takes the namespace from the channel by a `#` split (G13) | `redis.New` | N | 3+ | R/U |
| A10 | REDIS | D2, D8 | Redis adapter options (`key`, `host/port`, own pub/sub clients, `requestsTimeout`) | redis index.js:52 | PARTIAL | `redis.New` with targeted `WithAddr`, `WithPrefix`, `WithNetwork`, `WithPassword`, `WithDB` options in adapter/redis/options.go. **Defect**: normalization still drops DB and uses DB0. `redis.TestFactoryOptionsAndIndependentNamespaces` retains DB0; no TLS/user/sentinel. | `redis.New`, `redis.Option` | N | 1 | R |

## 5. Engine.IO server options (`engine.io/lib/server.js`)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| E1 | DONE V1-2 | D15 | `pingTimeout` (20000) **D** | server.js:40 | PARITY | `TestPingTimeoutIdleClient` (ping/messages/silent): a real websocket deadline closes only after PingInterval + PingTimeout without any packet; `TestHeartbeatDefaults` asserts 20 s | `WithPingTimeout` | N | 1 | T |
| E2 | DONE V1-2 | D15 | `pingInterval` (25000) **D** | server.js:41 | PARITY | `TestHeartbeatDefaults`: OPEN advertises PingInterval 25 s and PingTimeout 20 s | `WithPingInterval` | N | 0 | T |
| E3 | DONE V1-2 | D5 | `upgradeTimeout` (10000) **D** | server.js:42; socket.js:189 | PARITY | `WithUpgradeTimeout` defaults to 10 s; `TestUpgradeTimeout` closes the stalled probe within 1 s and verifies polling still works | - | Y | 1 | T |
| E4 | DONE V1-2 | D8, D1 | `maxHttpBufferSize` (1e6; also ws `maxPayload`) **D** | server.js:43,111 | PARITY (D9) | `TestMaxHTTPBufferSize`: exact-limit POST/message accepted, excessive POST is HTTP 413 and closes with transport error, excessive websocket message closes with 1009; default 1e6. POST failure uses 413 rather than destroying the HTTP connection (ROADMAP V1-2 contract) | - | Y | 1-2 | T |
| E5 | DONE V1-2 | D1 | `allowRequest(req, fn(err, ok))` **D** | server.js:46,135-167 | PARITY (D9) | RequestChecker runs for new handshakes; `TestEngineRequestValidation` pins HTTP 403 JSON with fixed Forbidden. Node reports the allowRequest error and uses plain-text 400 for an upgrade (P11; ROADMAP V1-2 contract) | `RequestChecker` | N | 1 | T |
| E6 | - | - | `transports` (order, default polling, websocket) **D** | server.js:44 | PARITY | `WithTransports(...transport.Transport)`; order is the upgrade path (server_options.go:82-90, manager.go:36-45) | `WithTransports` | - | 0 | R |
| E7 | DONE V1-2 | D5 | `allowUpgrades` **D** | server.js:45,122 | PARITY (D9) | `TestDisableUpgrades`: WithUpgrades(false) advertises [] and rejects an unsolicited probe with code 3; enabled sessions upgrade. Node permits an unsolicited probe (ROADMAP V1-2 contract) | `WithTransports` | Y | 1 | T |
| E8 | DONE V1-15 | D11 | `perMessageDeflate` **D** | server.js:50,107 | PARITY | `TestPerMessageDeflateNegotiated` checks extension on/off, encoded-size threshold and raw RSV1. `TestPerMessageDeflateLimit` rejects a 100 MB inflated message with 1009 and less than 20 MB allocations. `TestConformancePerMessageDeflate` records the negotiated upgrade for a 4 KB Node round trip. | `WithPerMessageDeflate`, `WithCompressionThreshold` | Y | 1 | T |
| E9 | DONE V1-15 | D11 | `httpCompression` (gzip/deflate for polling) **D** | server.js:51; polling.js:289-320 | PARITY | `TestPollingHTTPCompression` checks gzip/deflate negotiation, disabled compression, threshold, absent Accept-Encoding and per-packet false. | `WithHTTPCompression(false)`, `WithCompressionThreshold` | Y | 1 | T |
| E10 | DONE V1-15 | D11 | `cookie`, `cookiePath`, `cookieHttpOnly` (`io` cookie, sticky sessions) **D** | server.js:47-49,317-329 | PARITY | `TestHandshakeCookie` checks defaults, custom name/path, HttpOnly off, full attributes, disable, later polls and websocket-first absence. | `WithCookie`, `WithCookieEnabled(false)` | Y | 1 | T |
| E11 | - | - | `wsEngine` **D** | server.js:39 | N/A | selects a Node module; Go uses gorilla/websocket | - | - | 0 | R |
| E12 | - | - | `initialPacket` **D** | server.js:52; socket.js:72 | N/A | see N10 | - | - | 0 | R |
| E13 | - | - | `cors` **[not in ref: appears in engine.io 4.x]** | - | N/A | 3.6.2 has `handlePreflightRequest` + socket.io `origins` | - | - | 0 | R |
| E14 | DONE V1-2 | D1 | `attach` opts `path`, `handlePreflightRequest`, `destroyUpgrade`, `destroyUpgradeTimeout` **D** | server.js:440-499 | PARTIAL | Path is supplied by net/http; `TestEngineRequestValidation/preflight` pins code 2 for OPTIONS without sid, with no session created; existing-sid preflight remains the polling transport responsibility | - | N | 1 | T |
| E15 | DONE V1-2 | D8 | `generateId(req)` **D** | server.js:278 | PARITY | `TestRandomSessionID`: 1000 concurrent-generated, unique URL-safe ids with at least 120 random bits; the default uses crypto/rand; custom SessionIDGenerator is preserved | `SessionIDGenerator` | N (default) | 1 | T |
| E16 | - | - | `clientsCount` **D** | server.js:35 | PARITY | `TestEngineClients`: Count counts registered sessions including closed-not-removed sessions (D9); len(Clients()) gives the reference clientsCount | `Count`, `Clients` | - | 0 | T |
| E17 | DONE V1-16 | D16 | `clients` map **D** | server.js:34 | PARITY | `TestEngineClients`, `TestEngineConnState`: copied ID map includes open/closing sessions, excludes closed sessions before Remove | `Clients` | Y | 1 | T |
| E18 | - | - | `connection` event **D** | server.js:342 | PARITY | pull model `engineio.Server.Accept()` engineio/server.go:65 | `Accept` | - | 0 | R |
| E19 | DONE V1-17 | D16 | engine events `flush`, `drain`, socket `packet`, `packetCreate`, `heartbeat`, `upgrading`, `upgrade`, `error` **D** | socket.js | PARITY | `TestEngineSocketEvents`: polling batch, heartbeat, packet types, upgrade ordering and transport Error before close; `TestEngineServerEvents`: connection, initial headers, all response headers and rejection codes | `WithHooks` | Y | 1-2 | T |
| E20 | DONE V1-16 | D16, D10 | engine socket `close(discard)`, `send`, `readyState`, `upgraded`, `transport` **D** | socket.js:364,462 | PARITY | `TestEngineConnState`, `TestEngineClientState`, `TestSessionReadyState`: state around Close, transport and upgrade at the switch, client state after Close and ping-loop exit | `ReadyState`, `Transport`, `Upgraded`, `Close`, `NextWriter` | Y | 1 | T |
| E21 | - | - | `handleRequest`, `handleUpgrade` **D** | server.js:214,351 | PARITY | `ServeHTTP` handles both (engineio/server.go:104) | `ServeHTTP` | - | 0 | R |
| E22 | DONE V1-2 | D8 | `close()` closes all clients **D** | server.js:191 | PARITY | `TestServerCloseClosesAcceptedSessions` and `TestServerCloseClosesUnacceptedSessions`: accepted, queued and opening sessions are closed/removed; new handshakes after Close fail | `Close` | N | 1 | T |
| E23 | - | - | `EIO` query parameter check | server.js (none) | PARITY | neither checks it in 3.x (PROTOCOL.md) | - | - | 0 | R |

## 6. Engine.IO protocol v3 behaviour (`engine.io`, `engine.io-parser` 2.2.1)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| P1 | - | - | OPEN packet `sid/upgrades/pingInterval/pingTimeout` | socket.js:65 | PARITY | engineio/parser/parameters.go; engineio/internal/session/session.go Init | - | - | 0 | R |
| P2 | - | - | ping direction v3: client `ping`, server `pong` | socket.js:98-102 | PARITY | engineio/internal/session/session.go NextReader | - | - | 0 | R |
| P3 | DONE V1-2 | D1 | any packet resets the liveness timer | socket.js:93-95 | PARITY | `TestPingTimeoutIdleClient/messages`: only MESSAGE every 100 ms keeps a 150 ms liveness window open beyond 500 ms; stopping packets closes with ping timeout | - | N | 0-1 | T |
| P4 | DONE V1-2 | D1, D9 | upgrade probe `2probe`/`3probe`/`5`: pause polling on `5`, noop to the pending poll at a 100 ms check, polling kept on failure | socket.js:180-266 | PARITY | `TestUpgradeProbeWindow` (post/cadence/failed/timeout): POSTs work after 3probe, each packet arrives once, NOOP checks are 100 ms apart, only 5 pauses polling; failed/timed-out probes leave polling working | - | Y | 0-1 | T |
| P5 | DONE V1-1 | D4 | JSONP GET `___eio[<j>]("...")`, `j` reduced to digits | polling-jsonp.js:24,56-74 | ABSENT | removed by V1-1 (D4): `j` is ignored, a poll answers the ordinary payload and headers, nothing of `j` is written (`TestPollingIgnoresJSONPCallback`). Audit: `___eio[` + raw `j` + `]("`, `j` not sanitised | - | N | 1 | T |
| P6 | DONE V1-1 | D4 | JSONP POST (`application/x-www-form-urlencoded`, field `d`, escaped `\n`) | polling-jsonp.js:39-54 | ABSENT | removed with JSONP (D4): POST accepts only `application/octet-stream` or `text/plain; charset=utf-8` (polling/util.go:21-40); a form POST, with or without `j`, is HTTP 400 and reaches no session (`TestPollingRejectsJSONPFormPost`) | - | N | 1 | T |
| P7 | - | - | `b64=1` base64 text payload; `supportsBinary` otherwise | server.js:304-308; polling.js | PARITY | polling/server.go:31,126-131; payload/encoder.go b64 writer | - | - | 0 | R |
| P8 | DONE V1-18 | D15 | XHR CORS headers on every polling response | polling-xhr.js:48-62 | PARITY | `TestCORSDefault` verifies reflected Origin and credentials, or wildcard without Origin, on initial GET, session GET, POST and OPTIONS; cross-origin websocket handshakes and probes succeed. The review of 2026-10-18 listed this default (any Origin reflected with credentials, cross-origin upgrades accepted) as known: it is D15, not a defect. | `engineio.NewServer` | N | 1 (with S4) | T |
| P9 | DONE V1-8 | D7 | OPTIONS answer with `Access-Control-Allow-Headers: Content-Type` | polling-xhr.js:32-41 | PARITY | internal/conformance/transport_test.go: TestPollingOptions; OPTIONS on an accepted polling session returns 200, Content-Type allowance and an empty body | - | - | 0 | T |
| P10 | - | - | IE `X-XSS-Protection: 0` | polling.js:398-403 | PARITY | polling/server.go:65-67 | - | - | 0 | R |
| P11 | DONE V1-2 | D1, D9 | error replies JSON `{code,message}`, codes 0-4, CORS headers on 400, 403 for rejected, to a request that is not a websocket upgrade; a rejected upgrade gets `abortConnection`'s plain-text 400 with the code's message | server.js:72-86,243-268,351-358,509-526 | PARTIAL (D9) | `TestEngineRequestValidation`: JSON codes 0–4, HTTP 400/403; a rejected websocket upgrade gets the same JSON/status and a checker rejection always says Forbidden, unlike Node plain-text upgrade errors and allowRequest error messages (ROADMAP V1-2 contract); CORS headers are V1-18 | - | N | 1 | T |
| P12 | DONE V1-2 | D1 | handshake must be GET (`BAD_HANDSHAKE_METHOD`) | server.js:163-164 | PARITY | `TestEngineRequestValidation/post` and `/preflight`: a handshake without sid requires GET, otherwise HTTP 400 with code 2 Bad handshake method and no session | - | N | 0-1 | T |
| P13 | - | - | unknown transport / unknown sid / transport changed without upgrade give 400 | server.js:136-160 | PARITY | engineio/server.go:107-112,126-134,157-162 (`canUpgrade`) | - | - | 0 | R |
| P14 | - | - | Origin header with invalid chars rejected | server.js:143-149,561 | N/A | `net/http` rejects invalid header values at parse time | - | - | 0 | U |
| P15 | DONE V1-8 | D7 | websocket: one packet per frame, binary frame carries raw bytes, text for strings | websocket.js:76-103 | PARITY | internal/conformance/transport_test.go: TestWebsocketFraming; raw websocket text and binary packets in both directions, two separate frames, no polling framing or base64 | - | - | 0 | T |
| P16 | DONE V1-1 | D8, D7 | polling payload `<len>:<packet>` in UTF-16 code units, `b`+base64 for binary | engine.io-parser | PARITY | text payload (`b64=1`) in UTF-16 code units; the binary payload counted UTF-16 units too, where engine.io-parser 2.2.1 counts bytes, so non-ASCII text failed for a client polling without `b64=1` (the browser build of socket.io-client 2.x with XHR2; reproduced with engine.io-parser 2.2.1, not with a browser; in Node, engine.io-client sends `b64=1` and was not affected). V1-1 counts bytes (payload/encoder.go `writeBinaryHeader`, decoder.go `Read`); `TestPollingBinaryPayloadNonASCII` and the payload vectors use bytes produced by engine.io-parser 2.2.1 | - | - | 0 | T |
| P17 | DONE V1-8 | D7 | graceful close: flush buffered packets, `close` packet on polling | socket.js:462-485; polling.js:356-385 | PARITY | `TestPollingGracefulClose`: both framings, pending/next poll, accepted writer before CLOSE; session deadline/abort/shutdown/upgrade tests and root deadline/overflow tests preserve immediate forced close. Previously a pending poll received HTTP 500 EOF instead of CLOSE. | - | - | 0 | T |
| P18 | DONE V1-8 | D7 | overlapping poll / data requests rejected (`overlap from client`) | polling.js:81-88,126-132 | PARITY | internal/conformance/transport_test.go: TestPollingOverlaps; raw concurrent GET/POST rejection with writer/header barriers, accepted request still completes | - | - | 0 | T |

## 7. Socket.IO parser v4 (`socket.io-parser` 3.4.5)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| R1 | - | - | packet types 0-6, protocol 4 | parser index.js | PARITY | parser/packet.go:6-22 | - | - | 0 | R |
| R2 | - | - | text format `type[att-][nsp,][id][json]`, nsp query `?` | decodeString | PARITY | decoder.go:208-295; encoder.go:100-125. Encoded data packets end with `\n` (lifecycle_test.go:193-196, harmless to JSON clients) | - | - | 0 | R |
| R3 | - | - | ack ids | parser | PARITY | `Header.ID/NeedAck` | - | - | 0 | R |
| R4 | - | - | binary placeholders `{_placeholder,num}` + following binary frames, `BINARY_EVENT/ACK` | binary.js | PARITY | parser/buffer.go:18-62; decoder.go:139-157; type fold decoder.go:89-91 | `parser.Buffer` | - | 0 | R |
| R5 | - | - | `hasBin` auto-detection | has-binary2 | PARITY | `Buffer` values detected by the encoder | - | - | 0 | R |
| R6 | DONE V1-3 | D8 | `maxAttachments` (10) -> "too many attachments" | index.js Decoder | PARITY | `TestMaxAttachments`: headers above 10 (including huge/overflowing counts) fail before allocation or attachment reads; exactly 10 succeeds | - | N | 1 | T |
| R7 | DONE V1-3 | D1 | payload validation per type (`invalid payload`, `Illegal attachments`) | isPayloadValid | PARITY | `TestPayloadValidation`: packet-type JSON shapes match socket.io-parser 3.4.5; malformed payloads and out-of-range binary placeholder indices are rejected before dispatch | - | N | 1 | T |
| R8 | DONE V1-4 | D8 | ERROR packet encode/decode | parser | PARITY | ERROR string, object, array and null payloads round-trip as one value. Verified by `parser.TestErrorPacket`. | - | N | see S10 | T |
| R9 | DONE V1-4 | D5 | DISCONNECT packet write by server | socket.js:483 | PARITY | root and namespace DISCONNECT wire forms are 1 and 1/chat,. Verified by `parser.TestDisconnectPacket`. | - | N | see K20 | T |
| R10 | DONE V1-4 | D1 | encode failure -> `4"encode error"` | index.js:55 | PARITY | no partial packet precedes ERROR encode error and the next event is delivered. Verified by `TestEncodeErrorKeepsConnection`. | - | N | 0 | T |
| R11 | - | - | pluggable parser class | index.js:54 | N/A | see S6 | - | - | 0 | R |

## 8. Client (`socket.io-client` 2.5.0 / `engine.io-client` 3.5.6 vs `client/` and `engineio/client/`)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| C1 | - | - | `io(url, opts)` / `Manager` / `socket(nsp)` | client lib/index.js:36; manager.js:370 | PARTIAL | `client.New(addr, ...client.Option)` in client/client.go; manager/namespace behavior is covered by C2–C8 below | `client.New` | - | 0 | R |
| C2 | DONE V1-10 | D6 | multiplexing namespaces on one connection | manager.js:370 | PARITY | multiple namespace sockets over one Engine.IO connection; close isolation and once-only OnConnect. `client.TestClientNamespaces`, `client/internal/connection.TestClientLateConnectHandler`, `client/internal/connection.TestClientRootSocketAddedAfterConnect`; tagged `TestGoClientConformance` | `client.New`, `Client.Socket`, `Socket` | Y | 2 | T |
| C3 | DONE V1-11 | D6, D12 | reconnection (options + events `reconnect*`) | manager.js:127-190,519-580 | PARITY | backo2 retry delays, attempt/error/success/failure callbacks, timeout and cancellation, active namespaces only; `client.TestClientReconnect`, `client/internal/connection.TestClientReconnectCancellation`, tagged `TestGoClientReconnect` against Node 2.5.0 | `client.Option / With…`, `Client.OnReconnect*` | Y | 2 | T |
| C4 | DONE V1-10 | D6 | `timeout`, `connect_error`, `connect_timeout` | manager.js:191,221-290 | PARITY | bounded Connect, original dial errors, retries and discarded late OPEN; cancellation and concurrent callers. `client.TestClientConnectTimeout`, `client/internal/connection.TestClientTimeoutClosesLateConnection`, `client/internal/connection.TestClientCloseCancelsPendingConnect`, `client/internal/connection.TestClientConcurrentConnectAndRetry` | `client.WithTimeout`, `ErrConnectTimeout` | Y | 1 | T |
| C5 | DONE V1-10 | D6 | websocket transport and upgrade | engine.io-client | PARITY | polling, websocket and ordered asynchronous probe; no lost/reordered packets, bounded failures and post-switch deadlines. `client.TestClientWebsocketUpgrade`; `engineio/client.TestClientUpgradeOrder`, `TestClientUpgradeProbeFails`, `TestClientUpgradeDeadline`, `TestClientUpgradeTransportErrors`; tagged `TestGoClientConformance` | `client.WithTransports`, `engineio/client.Dialer.Upgrade/UpgradeTimeout` | Y | 1 | T |
| C6 | DONE V1-10 | D6 | `query`, `path`, `extraHeaders` options | manager.js:44 | PARITY | custom engine path, merged query and cloned headers on polling and websocket; namespace CONNECT query. `client.TestClientOptions`; tagged `TestGoClientConformance` | `client.WithPath/WithQuery/WithHeader` | Y | 1 | T |
| C7 | - | - | `emit` with ack, `on(event)`, `connect/disconnect/error` events | socket.js:136 | PARITY | `client.Client.Emit`, `OnEvent`, `OnConnect`, `OnDisconnect`, `OnError` in client/client.go; single handler per event | - | - | 0 | R |
| C8 | DONE V1-11 | D6, D10 | `once/off`, `ping/pong` events, `socket.id/connected` | socket.js | PARITY | listeners and removers, IDs and Connected across namespace lifecycles on both sides, heartbeat observers; `client.TestClientSocketState`, `TestClientReconnect` | `Socket.ID/Connected/On/Once/Off`, `Namespace.Connected`, `Client.OnPing/OnPong`, `engineio/client.Dialer.OnPing/OnPong` | Y | 1 | T |
| C9 | DONE V1-10 | D6 | emit before connect is buffered | socket.js:345 | PARITY | bounded pre-connect buffer flushed after CONNECT in order; overflow drops with WARN. `client.TestClientEmitBuffered` | `Client.Emit`, `Socket.Emit` | N | 1 | T |
| C10 | DONE V1-10 | D6 | client sends DISCONNECT on close | socket.js:398-420 | PARITY | namespace DISCONNECT precedes engine close, including a full application queue; server sees client namespace disconnect. `client.TestClientCloseSendsDisconnect`, `client/internal/connection.TestClientDisconnectWithFullQueue`; tagged `TestGoClientConformance` | `Client.Close`, `Socket.Close`, `Conn.Disconnect` | N | 1 | T |
| C11 | DONE V1-11; DONE V1-15 | D6, D11, D10 | flags `volatile`, `compress`, `binary` | socket.js:422-440 | PARITY | `client/internal/connection.TestClientVolatile` and `client/internal/connection.TestClientVolatileBeforeConnect` check the Go-only volatile extension (D10). `TestClientCompress` checks offered negotiation and raw text/binary flags against Go; `TestConformanceClientPerMessageDeflate` checks a 4 KB compressed round trip against Node 2.5.0. | `Namespace.Volatile`, `Namespace.Compress` | Y | see K | T |

## Go-only (implemented-only) surface, not in the reference

`engineio.WithLogger`, `WriteBufferSize` with `ErrWriteBufferFull` (bounded per-conn queue
that closes the conn on overflow, errors.go:25-33), `ConnInitor`, `Server.Remove(sid)`,
`Server.ClearRoom`, `Server.RoomLen`, Redis-backed room queries, `Conn.Context/SetContext`,
`parser.Buffer`. The bounded queue closes a healthy slow client where Node would buffer
without limit; it is a behaviour difference with no reference counterpart. Over polling it
also closes clients that are not slow, because each GET carries one packet (G24).

## Notable findings outside the matrix rows

1. Default session id is a sequential counter (E15): ids are guessable.
2. Polling JSONP callback `j` is echoed unsanitised (P5).
3. No cap on attachment count (R6, measured) and no payload size cap (E4).
4. Redis database selection is normalized to DB0 (A10).
5. `BroadcastToNamespace` duplicates per room (S12, already pinned by a test).

Findings 1 to 5 are rows E15, P5, R6 with E4, A10 and S12; their plan is in those rows.

## Defects reported without a matrix row

Added by the owner after the audit, so they carry no reference item and no audit evidence:
`Ver` is `U` until the closing PR adds a failing test (the stage rule: a fix carries a test that
fails without it). They have no `Status`; the closing PR gives each the evidence it found.

| ID | Plan | Decision | Defect | Closing PR note | Ver |
| --- | --- | --- | --- | --- | --- |
| B1 | DONE V1-3 | D8 | `Emit(ev, nil)` panics | `TestEmitNil`: nil alone and before another argument is encoded as JSON null and delivered | T |
| B2 | DONE V1-3 | D8 | an event argument of the wrong type closes the connection | `TestWrongTypeArgumentKeepsConnection`: event, binary event and ACK Go type mismatches reach OnError; the next valid event receives its ACK on the same connection. `TestWrongTypeConsumesBinaryAttachments` verifies frame alignment | T |
| B3 | REDIS | D2 | binary arguments across Redis instances (listed by the owner as Redis work; the defect is not characterised yet) | Stage V1R | U |
| B4 | REDIS | D2 | `RoomLen` and `Rooms` wait the full 5 s when a peer has not registered or does not answer (1.R known limitation, ROADMAP 1.K) | Stage V1R | R |

## Findings of the review of 2026-10-18

An independent review of the v1 implementation at `3db9751` (`master` after #133), in six
dimensions: concurrency, Engine.IO protocol, Socket.IO layer, security and limits, API and
errors, tests and build. File and line references are to that tree. "verified" in the
evidence marks a finding that a second, independent reviewer checked and could not refute;
its `Severity` is that reviewer's rating. The other rows come from items the review recorded
without a second check: `low` marks one of its low items, `-` an item it did not rate. A defect
found in two dimensions is one row. Earlier findings that the review rechecked and found fixed
are not recorded. `Status` is the matrix status of the behaviour the row names: `PARTIAL`
while the defect is open, `PARITY` where the reference behaves the same (G16) or only the
test is missing (G29). `Ref` is the reference behaviour where one exists, and `Ver` is `T`
where the review reproduced the defect with a throwaway program. Every `BUG` row is closed
test first (CONTRIBUTING.md, *Pull requests*); wave V1G of ROADMAP Stage V1 orders the PRs.
The DONE rows below contain permanent regression evidence for the corrected implementation;
the defect descriptions retain the original failure scenarios. G30–G31 were found during
the follow-up validation. T1–T3 remain a separate change.

| ID | Plan | Decision | Severity | Defect | Ref | Status | Go evidence | API? | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| G1 | DONE V1-21 | D1 | high | a second `transport=websocket` request (a plain GET or a websocket upgrade) with the sid of a live websocket session hangs its handler until the session closes | server.js: a plain GET with the session's transport passes `verify` (:158) and goes to `Transport.onRequest` (:228, transport.js:60), which stores it and answers nothing; an upgrade request is accepted and then closed by `onWebSocket` (:378-418) for an upgrading or upgraded client, and for a session that started on websocket and never upgraded it starts `maybeUpgrade` (socket.js:180-195), which closes the new transport after `upgradeTimeout`. No case answers code 3, and Node parks no handler, so the answer V1-21 gives is Go-only | N/A | Duplicate websocket requests return HTTP 400/code 3 (Go-specific), and canceled handlers return. `TestDuplicateWebsocketSessionRequestReturns`. | N | T |
| G2 | DONE V1-22 | D1, D17 | medium | the engine session is never removed when the root connect fails (no root handler, a root `OnConnect` that overflows `WithWriteBufferSize`, a root adapter that failed), and no test fails when the removal of finished sessions is dropped | server.js handshake: `socket.once('close')` deletes the client | PARITY | Failed and successful sessions leave the engine registry. `TestRegressionG2FailedRootRemovesEngineSession`, `TestSuccessfulSessionRemovedAfterPeerClose`. | N | T |
| G3 | DONE V1-23 | D1 | medium | an application `Disconnect()` racing a client re-CONNECT of the same namespace removes the new socket's rooms, its ID room included | - | PARITY | Replacement CONNECT waits for old namespace cleanup while other namespaces remain usable. `TestRegressionG3OldDisconnectPreservesNewRooms`. | N | T |
| G4 | DONE V1-23 | D1 | medium | `Conn.Close` from another goroutine runs a namespace's `disconnecting` and `OnDisconnect` while its `OnConnect` still runs, and writes CONNECT for a socket that was never accepted | - | PARITY | Close defers disconnect callbacks until admission returns; rejected admission never sends CONNECT. `TestRegressionG4CloseDoesNotDisconnectDuringConnect`, `TestRejectedAdmissionDuringCloseDoesNotConnect`. | N | T |
| G5 | DONE V1-23 | - | medium | the adapter factory of a new dynamic namespace (a Redis dial, up to 10 s) runs under the server-wide `createMu`: one slow creation stalls other clients' CONNECTs to new dynamic namespaces, `Of`, `SetAdapter` and `Close` | - | PARITY | Factories run outside the server lock; concurrent creation shares its result and shutdown disposes late adapters. `TestRegressionG5SlowFactoryDoesNotBlockOtherNamespace`, `TestFactoryCreationSharedAndClosedAfterCompletion`. | N | T |
| G6 | DONE V1-24 | D1 | medium | emitted values are encoded later, on the writer goroutine: a map changed after `Emit` or a broadcast aborts the process (`concurrent map iteration and map write`), and the client gets the value of write time | socket.js `emit`: the packet is encoded inside the call | PARITY | Prepared packets snapshot JSON and binary data before queue admission, including client pre-connect buffering. `TestRegressionG6EmitSnapshotsMap`, `TestEmitSnapshotsBinaryAndNestedPayload`; client snapshot tests. | N | T |
| G7 | DONE V1-24 | - | medium | the encoder takes any struct type named `Buffer` for `parser.Buffer`: emitting `*bytes.Buffer`, or an own `Buffer` type behind a pointer or in a slice, panics on the writer goroutine and the process exits | - | N/A | Only the exact parser Buffer type is an attachment. `TestV1AuditForeignBufferMustNotPanic`. | N | T |
| G8 | DONE V1-24 | - | medium | an emitted value with a cycle (a pointer to itself, a map that holds itself) overflows the stack in the encoder and aborts the process, where `json.Marshal` returns an error | - | N/A | Cyclic containers return an encoding error; shared acyclic children remain valid. `TestReviewG8CyclicValueReturnsError`, `TestEncodingContainerCyclesAndSharedChildren`. | N | T |
| G9 | DONE V1-25; DONE V1-28 | D1 | medium | a binary EVENT or ACK for a namespace the socket has not joined (or without a handler) leaves its attachments unread, desynchronises the decoder and closes the whole connection, on the server and on the Go client | client.js `ondecoded`: the packet is reassembled, then dropped (`no socket for namespace`) | PARITY | Discard drains all attachments before the next header. `TestIgnoredBinaryEventsConsumeTheirAttachments`, `TestClientIgnoredBinaryPacketKeepsConnection`. | N | T |
| G10 | DONE V1-26 | D1 | low | a PING in a POST is answered on the reader before the POST is released: during an upgrade probe with no pending GET, the POST, `Pause` and the upgrade stay blocked until the write deadline (45 s by default) | socket.js:98-100: the pong is queued and the POST returns at once | PARITY | PING releases its POST before waiting for a pong poll. `TestPingPostReleasesBeforePongPoll`. | N | T |
| G11 | DONE V1-26 | D1 | low | an aborted long-poll is not detected: the next packet is reported written into the dead response and is lost, and the session closes at the ping timeout instead of with a transport error | polling.js:96-98: `req` `close` gives `poll connection closed prematurely` | PARITY | Canceled GET fails the transport without releasing an active writer lease early. `TestCanceledPollFailsTransportBeforeNextWrite`, `TestCanceledBatchKeepsActiveWriterLease`. | N | T |
| G12 | DONE V1-24 | - | low | `parser.Buffer` by value, in a value struct or as a map value cannot be emitted (`can't get Buffer address`, the client gets `4"encode error"`), although the decoder fills those shapes | - | N/A | Value Buffers and nested/map forms encode binary attachments. `TestReviewG12BufferValue`. | N | T |
| G13 | DONE V1-27 | - | medium | the Redis adapter takes the namespace from the channel by a `#` split: a client-created namespace containing `#` injects broadcasts into another namespace on the other instances, and a namespace with `#` never receives remote broadcasts | - (Go-only channel format, A9) | N/A | Opaque namespace prefixes isolate channels containing separators and glob characters. `TestReviewG13NamespaceChannelIsolation`. | N | T |
| G14 | DONE V1-27 | - | medium | the Redis adapter dials two connections and starts a goroutine per namespace and never releases them, so client-created dynamic namespaces exhaust the Redis connections | redis index.js: one pub/sub client pair per instance | PARITY | A factory shares one pool and subscriber across namespace adapters; the last adapter closes the resources. `TestFactorySharesRedisConnectionsAcrossNamespaces`, `TestSharedFactoryReconnectsRemainingNamespaces`. | N | T |
| G15 | DONE V1-27 | - | - | a Redis broadcast to the room `""` reaches every socket of the namespace on the other instances | - | N/A | Explicit empty-room selection is distinct from namespace delivery, including across instances. `TestReviewG15EmptyRoomDoesNotBroadcastNamespace`. | N | T |
| G16 | DONE V1-28 | - | low | the Go client has no message size limit: a hostile server, or a man in the middle on `ws://` or `http://`, makes it buffer one message of any size | engine.io-client 3.5.6 has none either; the server has E4's limit | N/A | Client limits default to 1 MiB and apply before OPEN, messages and upgrade probes are decoded. `TestClientRejectsOversizedOpen`, `TestClientMessageLimitOption`; engine client boundary and upgrade tests. | Y | T |
| G17 | DONE V1-30 | D1 | - | a server without a root `OnConnect` closes every connection (and leaks it, G2), where the reference always has `/` and PROTOCOL.md says the root is joined automatically | index.js: `this.sockets = this.of('/')` | PARITY | The root namespace is initialized without requiring OnConnect registration. `TestRegressionG17RootAlwaysExists`. | N | T |
| G18 | DONE V1-25 | - | - | a panic in `OnConnect` or a namespace middleware, or `Use(nil)`, crashes the process, where a panic in an event handler is recovered and reported | - | N/A | Connect and middleware panics become errors; nil middleware is ignored. `TestRegressionG18ConnectPanicIsContained`, `TestConnectMiddlewarePanicAndNil`; client connect panic tests. | N | T |
| G19 | DONE V1-30 | D1 | - | a client fires the `disconnecting` lifecycle listener with any reason by sending an event of that name; the socket stays connected | socket.js `emit`: reserved names (`exports.events`) | PARITY | Incoming reserved events are discarded with their binary attachments. `TestRegressionG19PeerCannotDispatchLifecycle`, `TestIgnoredBinaryEventsConsumeTheirAttachments`. | N | T |
| G20 | DONE V1-30 | - | low | a variadic event handler or ack callback is accepted at registration but gets the first JSON argument decoded as a slice, so ordinary events fail, and the array form closes the connection | - | N/A | Flattened variadic event and ACK arguments decode and invoke correctly; incompatible listener conventions are rejected. `TestVariadicPayloadAndAckCallbacks`, `TestListenerVariadicConventionsMustMatch`; client variadic tests. | N | T |
| G21 | DONE V1-30 | D1 | - | the server writes an ACK without an id when a handler returns values for an event that asked for no ack, and an empty ACK for an event nobody listens to | socket.js:376-393: an ack only for a packet with an id | PARITY | Automatic ACK requires both a requested ID and an actual listener. `TestRegressionG21NoUnrequestedAck`, `TestIgnoredBinaryEventsConsumeTheirAttachments`; client ACK tests. | N | T |
| G22 | DONE V1-31 | - | - | a negative `WithPingTimeout` or `WithPingInterval` is kept and advertised in OPEN | - | N/A | Nonpositive heartbeat durations resolve to advertised defaults. `TestReviewG22NegativeHeartbeatNotAdvertised`. | N | T |
| G23 | DONE V1-30 | - | - | the legacy `Server.JoinRoom` and `LeaveRoom` take a nil `Conn` (a panic), a closed one or one of another namespace without a check | - | N/A | Room operations reject nil, foreign-namespace and disconnected sockets. `TestRegressionG23NilRoomConnection`, `TestRoomMethodsRejectForeignAndDisconnectedSockets`. | N | T |
| G24 | DONE V1-31 | D1 | - | polling answers each GET with one packet, so a polling client gets one packet per round trip and is closed once the bounded write queue fills: at 100 messages/s and a 50 ms round trip, where websocket receives all | polling.js `send`: the whole write buffer in one payload | PARITY | Polling coalesces bounded ordered batches, preserving writer leases, CLOSE order and compression opt-outs. `TestPollingBatchesOrderedPacketsInOneResponse`, `TestPollingBatchContainsBinaryAndCloseInOrder`, `TestPollingBatchHasFinitePacketBudget`; mixed-compression tests. | N | T |
| G25 | DONE V1-31 | D1 | low | the text payload decoder accepts lengths engine.io-parser 2.2.1 rejects: an empty length reads as 0, an empty packet takes the next packet's first byte as its type, a truncated packet is accepted, an overflowing length wraps | engine.io-parser `decodePayload` | PARITY | Invalid/overflowing lengths and truncation fail; zero-length packets are skipped. `TestReviewG25InvalidPayloadLengths`, `TestReviewG25ZeroLengthSkipsEmptyPacket` and payload fuzz tests. | N | T |
| G26 | DONE V1-31 | D1 | low | an unknown Engine.IO packet type is skipped instead of closing the session with a parse error | - | PARITY | Unknown packet types terminate sessions with parse errors and retain underlying limit failures. `TestReviewG26UnknownPacketClosesSession`. | N | T |
| G27 | DONE V1-31 | D1 | low | an empty binary (`application/octet-stream`) POST makes the payload decoder terminal and closes the session, where engine.io reads it as no packets | engine.io-parser `decodePayloadAsBinary` | PARITY | Empty binary POST is accepted; partial headers still fail. `TestReviewG27EmptyBinaryPostKeepsSession`, `TestEmptyAndTruncatedPayloadBoundaries`. | N | T |
| G28 | DONE V1-30 | D1 | low | `Conn.Emit` sends the reserved event names `error`, `connect`, `disconnect` and `disconnecting` to the client | socket.js `emit`: a reserved name is emitted locally, not sent | PARITY | Reserved emits dispatch locally without lifecycle transitions or wire events. `TestRegressionG28ReservedEmitStaysLocal`, `TestReservedEmitCallsLocalListenersOnce`. | N | T |
| G29 | DONE V1-31 | D17 | low | the guard that rejects a second concurrent upgrade probe has no test: removing it fails nothing | - | PARITY | A simultaneous second upgrade probe closes without displacing the first. `TestConcurrentUpgradeProbeClosesOnlySecond`; removing the guard fails this test. | N | T |
| G30 | DONE V1-24 | - | medium | encoder drops a frame Close/flush error and reports successful delivery | - | N/A | `TestV1AuditEncoderMustReturnFrameCloseError` verifies propagation; primary write errors retain precedence. | N | T |
| G31 | DONE V1-25 | - | medium | decoder drops the final frame Close/release error and dispatches a packet whose consumption failed | - | N/A | `TestV1AuditDecoderMustReturnFrameCloseError`, `TestDecoderAttachmentErrorsAndPrecedence` verify error propagation and precedence. | N | T |

## Typed API (D18)

D18 (ROADMAP Stage V1) requires a typed event API in v1 before `v1.5.0`: additive generic
functions over the current reflective dispatcher, with the names and semantics of the v2
declarations in `v2/event.go` (the inventory of [API.md](API.md)). The reference has no
typed API, so these rows have no `Ref`; the v1 signatures are `### V1-29 contract` of ROADMAP
Stage V1, which follows as V1-0's contracts did and which this file does not anticipate.

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| T1 | ADD V1-29 | D18 | typed event descriptor and handlers: v2 `NewEvent[T]`, `Event[T].Name`, `Event.Handle` (server) and `Event.HandleClient` (client) | - | ABSENT | handlers are reflected `func(Conn, args...)` values (`internal/namespace/handlers.go`, `client/internal/namespace/handlers.go`); no generic descriptor | `OnEvent`, `ServerNamespace.On` | Y | 1 | R |
| T2 | ADD V1-29 | D18 | typed emit: v2 `Event.Emit` to one socket and `Event.EmitTo` to a broadcast | - | ABSENT | `Conn.Emit` and the broadcast methods take `...interface{}` | `Emit`, `ServerNamespace.Emit` | Y | 1 | R |
| T3 | ADD V1-29 | D18 | typed acknowledgements: v2 `NewAckEvent[T, R]`, `AckEvent.Name`, `AckEvent.Handle`, `AckEvent.HandleClient` and `AckEvent.EmitWithAck` | - | ABSENT | acks are a trailing reflected `func(...)` of `Emit` or an `AckFunc` parameter (K26, K27) | `Emit`, `AckFunc` | Y | 1 | R |
