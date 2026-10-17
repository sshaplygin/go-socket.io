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
  change; the plan applies the same rule to the exported `Broadcast` and to `engineio.Conn`
  (E20, D16). `D10` in `Decision` marks the rows
  whose plan puts a method on `Conn`, `Namespace` (the per-connection view embedded in `Conn`),
  `Broadcast` or `engineio.Conn`. The V1-0 contracts are the list of those methods: a contract that puts one on
  an untagged row adds `D10` to it in the same PR. Untagged on purpose: the server namespace
  object of S8 (S14, S16, N2, N4), the ack parameter of a handler (K26), handler registration on
  the server (K28), the flags of server broadcasts (S21: `Server` is a struct; the flags reach
  `Broadcast` through the methods of A4).
- `Plan` is `<kind> V1-<n>` with the PR of ROADMAP Stage V1 that closes the row, several
  entries separated by `;`. Kinds: `BUG` fixed as a bug (a behaviour change); `ADD` new public
  API; `CHG` behaviour or internal change without new API; `TEST` a test verifies an unverified
  claim and the row changes only if the test disagrees; `DROP` declared unsupported and
  removed; `DEV` documented deviation, not closed in v1; `REDIS` the Redis follow-up stage;
  `-` nothing to do. The PR that closes a row rewrites the
  kind to `DONE`, keeps its number, and adds the comment `// Covers <ID>` directly above the test function that closes it (the Stage V1
  DoD finds the function with `go test -list`).
- `Decision` names the owner decision behind the plan: `D1` to `D17`, all settled, in ROADMAP
  Stage V1, which owns their text; no decision is pending.

## 1. Server (`socket.io/lib/index.js`)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| S1 | - | - | `new Server(srv, opts)` | index.js:43 | PARITY | `NewServer(*engineio.Options)` server.go:35 | `NewServer` | - | 0 | R |
| S2 | - | - | `path` option / `path()` | index.js:210 | PARITY | Server is an `http.Handler` (server.go:117); path is the mux pattern | mux registration | - | 0 | R |
| S3 | - | - | `serveClient` (serves `/socket.io.js`) | index.js:105,335 | N/A | JS client bundle serving; examples serve static files with `http.FileServer` | - | - | 0 | R |
| S4 | ADD V1-18 | D15 | `origins` option / `origins()` (default `*:*`, list or `fn(origin, cb)`) | index.js:69-95,243 | PARTIAL | no server-level setting; per-transport `CheckOrigin func(*http.Request) bool` polling/transport.go:15, websocket/transport.go:35. Default `nil`: polling sends no CORS header (polling/server.go:75), websocket uses gorilla same-origin default | `polling.Transport.CheckOrigin`, `websocket.Transport.CheckOrigin` | Y (the option that disables the default) | 1 | R |
| S5 | ADD V1-7 | D5 | `adapter(v)` pluggable adapter class | index.js:224 | PARTIAL | only `Server.Adapter(*RedisAdapterOptions)` server.go:75; `Broadcast` interface is exported (broadcast.go:9) but cannot be injected (namespace_handler.go:25-38) | `Server.Adapter` | Y | 2 | R |
| S6 | - | - | `parser` option (custom parser) | index.js:54 | N/A | fixed parser package; custom parsers not a v1 goal (msgpack is a v2 roadmap item) | - | - | 0 | R |
| S7 | - | - | `attach` / `listen(srv or port)` | index.js:259 | PARITY | `ServeHTTP` + `Serve()` server.go:117,172 (idiomatic Go; `listen(port)` convenience not needed) | `Server.ServeHTTP`, `Serve` | - | 0 | R |
| S8 | ADD V1-5 | D5 | `of(name[, fn])` static namespace, returns `Namespace` | index.js:453-476 | PARTIAL | namespaces created lazily by `OnConnect/OnEvent/...(ns, ...)` server.go:122-165,424; no `Namespace` object is returned | `Server.OnConnect(ns, f)` | Y (`Of`) | 2 | R |
| S9 | ADD V1-7 | D5 | `of(regex)` / `of(fn)` dynamic namespaces, `ParentNamespace`, `checkNamespace` | index.js:454-464,174-200; parent-namespace.js | ABSENT | none; a CONNECT to an unregistered namespace ends the connection (connection_handlers.go:93-98) | - | Y | 2-3 | R |
| S10 | DONE V1-4 | D8 | CONNECT to unknown namespace answers `ERROR "Invalid namespace"`, connection stays open | client.js:69-76 | PARITY | unknown CONNECT replies with ERROR and a following root ACK succeeds. Verified by `TestUnknownNamespaceKeepsConnection`. | - | N | 1 | T |
| S11 | ADD V1-5 | D5 | `use(fn)` namespace middleware `fn(socket, next)`, error becomes `ERROR` packet | index.js:509; namespace.js:102,119,172-176 | ABSENT | nearest: `OnConnect` returning an error (namespace_handler.go:89-91); that closes the whole connection and sends no ERROR packet (connection_handlers.go:116-121) | `OnConnect` error | Y | 2 | R |
| S12 | DONE V1-3 | D8 | `emit(ev, ...)` to all sockets of `/` (once per socket) | namespace.js:217; adapter 122-163 | PARITY | `TestNamespaceBroadcastOnce`: two sockets in several rooms receive a single namespace broadcast followed by a distinct marker; `TestLifecycleRootNamespace` also asserts no duplicate | `Server.BroadcastToNamespace` | N | 1 | T |
| S13 | CHG V1-6; REDIS | D1, D2 | `to(room)/in(room).emit` (one room, or union of several rooms with dedupe) | namespace.js:147; adapter 122 | PARTIAL | `BroadcastToRoom` server.go:242: single room only, no union, no dedupe | `Server.BroadcastToRoom` | N | 1 | R |
| S14 | ADD V1-5 | D5 | `send` / `write` (emits `message`) | namespace.js:255 | ABSENT | sugar only; equivalent `BroadcastToNamespace(ns, "message", ...)` | - | Y (sugar) | 0-1 | R |
| S15 | ADD V1-6; REDIS | D5, D2 | `clients(cb)` list of socket ids (all, or `in(room)`) | namespace.js:270; adapter 173 | PARTIAL | no id list; `ForEach(ns, room, f)` server.go:317 needs a room, `RoomLen`/`Rooms` count only | `Server.ForEach`, `RoomLen` | Y | 1 | R |
| S16 | ADD V1-5 | D5 | `nsp.connected`, `nsp.sockets` maps | namespace.js:55-56 | ABSENT | none | - | Y | 1 | R |
| S17 | BUG V1-5 | D8 | `close(fn)` closes all clients + http server | index.js:485-499 | PARTIAL | `Server.Close` server.go:101 stops accepting (`Serve` returns nil) and stops Redis; **does not close open sessions** (doc server.go:95-97; engineio/server.go:53-55) | `Server.Close`, example `_examples/graceful-shutdown` | Y (Shutdown) | 1-2 | R |
| S18 | DONE V1-16 | D16 | `engine` / `eio` property | index.js:425 | PARITY | `TestServerEngine`: stable, non-nil handle of the engine created by NewServer | `Server.Engine` | Y | 1 | T |
| S19 | - | - | `set(key,val)` legacy BC, `bind`, `onconnection`, `httpServer`, `checkRequest` | index.js:143,424-443 | N/A | deprecated or JS plumbing | - | - | 0 | R |
| S20 | - | - | event `connection` / `connect` | namespace.js:189-190 | PARITY | `OnConnect(ns, func(Conn) error)` server.go:122; one handler per namespace, a second call replaces it (namespace_handler.go:55-57) | `OnConnect` | - | 0 | R |
| S21 | ADD V1-6; ADD V1-15; REDIS | D5, D11, D2 | flags `volatile`, `local`, `json`, `compress(b)`, `binary(b)` on server | index.js:509-523; namespace.js:32 | ABSENT | none. `json` is a no-op in 2.5.0 (N/A); `binary` is decided by `parser.Buffer` in Go (N/A); `volatile`, `compress`, `local` ABSENT, see K18-K20 | - | Y | see K | R |

## 2. Namespace (`socket.io/lib/namespace.js`)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| N1 | - | - | `name` | namespace.js:53 | PARITY | `Conn.Namespace()` namespace_conn.go:64 | `Conn.Namespace` | - | 0 | R |
| N2 | ADD V1-5 | D5 | `use` | namespace.js:102 | ABSENT | same as S11 | - | Y | see S11 | R |
| N3 | CHG V1-6 | D1 | `to/in/emit/send/clients` per namespace | namespace.js:147-279 | PARTIAL | server methods take the namespace as a string argument (server.go:206-323); same gaps as S12-S15 | `Server.Broadcast*` | - | see S12-S15 | R |
| N4 | ADD V1-5 | D5 | `connected`, `sockets`, `ids`, `rooms` | namespace.js:55-59 | ABSENT | none | - | Y | see S16 | R |
| N5 | ADD V1-7 | D5 | `adapter` handle (`nsp.adapter.rooms/sids`) | namespace.js:91 | ABSENT | per-namespace `Broadcast` is internal (namespace_handler.go:14) | - | Y | see S5 | R |
| N6 | - | - | events `connect`, `connection` | namespace.js:189 | PARITY | see S20 | `OnConnect` | - | 0 | R |
| N7 | TEST V1-6 | D1 | `emit` with callback throws "Callbacks are not supported when broadcasting" | namespace.js:229 | PARTIAL (differs) | `BroadcastTo*` pass `args` to each `Conn.Emit`, so a trailing func would register one ack callback per conn (broadcast.go:86-90, namespace_conn.go:77-90). Not executed | - | N | 0-1 | U |
| N8 | ADD V1-7 | D5 | namespace CONNECT carrying a query (`/nsp?x=1`) available as `handshake.query` | client.js:208; socket.js:117-120 | PARTIAL | decoder parses it into `Header.Query` (decoder.go:268-273) and nothing reads it (grep: only that assignment); noted in docs/PROTOCOL.md | - | Y | 1 | R |
| N9 | DONE V1-4 | D1 | namespace CONNECT reply wire form `0/nsp,` | socket.js:311 | PARITY | namespace CONNECT replies with exactly 0/chat,. Verified by `TestNamespaceConnectPacket`. | - | N | 1 | T |
| N10 | - | - | root CONNECT merged into the handshake (`initialPacket`) | index.js:295-301; engine socket.js:72 | N/A | optimisation only; Go sends `0` right after OPEN (connection.go:223-229). Same client-visible result | - | - | 0 | R |

## 3. Socket (`socket.io/lib/socket.js`, `client.js`)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| K1 | CHG V1-5 | D15 | `id` (`/` : engine id, other nsp: `nsp#id`) | socket.js:64 | PARTIAL | `Conn.ID()` returns the engine sid for every namespace (connection.go:60) | `Conn.ID` | N | 0-1 | R |
| K2 | - | - | `rooms` | socket.js:67 | PARITY | `Conn.Rooms()` namespace_conn.go:114; includes the sid room (lifecycle_test.go:75) | `Conn.Rooms` | - | 0 | R |
| K3 | - | - | `client` | socket.js:65 | N/A | no per-engine `Client` object in Go; conn-level state is internal | - | - | 0 | R |
| K4 | ADD V1-7 | D5, D10 | `conn` (engine socket: transport, readyState, upgraded) | socket.js:66 | PARTIAL | `Conn` exposes only `ID/URL/LocalAddr/RemoteAddr/RemoteHeader` (connection.go:60-65); `Session.Transport()` (session.go:101) is not reachable | - | Y | 1 | R |
| K5 | ADD V1-7 | D5, D10 | `request` (`http.IncomingMessage`) | socket.js:102 | ABSENT | workaround: `Options.ConnInitor(*http.Request, engineio.Conn)` server_options.go:24, engineio/server.go:153 + `SetContext`. Root conn copies the context (connection.go:219-221); namespaces created later do not (connection_handlers.go:109) (not run) | `ConnInitor`, `Conn.Context` | Y | 1 | R/U |
| K6 | - | - | `handshake.headers` | socket.js:122 | PARITY | `Conn.RemoteHeader()` connection.go:65 | `RemoteHeader` | - | 0 | R |
| K7 | ADD V1-7 | D5, D10 | `handshake.query` (request query merged with nsp query) | socket.js:116-129 | PARTIAL | `Conn.URL().Query()`: engine request query only; nsp query dropped (N8) | `Conn.URL` | Y | 1 | R |
| K8 | - | - | `handshake.address` | socket.js:124 | PARITY | `Conn.RemoteAddr()` | `RemoteAddr` | - | 0 | R |
| K9 | - | - | `handshake.url` | socket.js:128 | PARITY | `Conn.URL()` (request URL, no scheme/host) | `URL` | - | 0 | R |
| K10 | ADD V1-7 | D5, D10 | `handshake.time`, `issued`, `secure`, `xdomain` | socket.js:123-127 | ABSENT | none (derivable from request but not exposed) | - | Y | 1 (with K7) | R |
| K11 | ADD V1-6 | D5, D10 | `join(room \| rooms[], cb)` | socket.js:236 | PARTIAL | `Conn.Join(room string)` only: one room, no callback, no error (namespace_conn.go:102) | `Join` | Y (variadic) | 1 | R |
| K12 | - | - | `leave(room, cb)`, `leaveAll()` | socket.js:269,287 | PARITY | `Leave`, `LeaveAll` namespace_conn.go:106-112 | same | - | 0 | R |
| K13 | - | - | `emit(ev, ...args[, ack])` to this client | socket.js:140-181 | PARITY | `Conn.Emit` namespace_conn.go:68-100; trailing func is the ack callback (not sent), called on ACK | `Emit` | - | 0 | R |
| K14 | ADD V1-6 | D5, D10 | `socket.broadcast.emit` / `socket.to(room).emit` (everyone except sender) | socket.js:170-175,191 | ABSENT | workaround: `ForEach` + id compare, as `others()` in `_examples/default-http/chat.go:60-67`, whose comment says "the library has no broadcast that skips the sender". **This is the upstream-chat gap** | - | Y | 2 | R |
| K15 | ADD V1-5 | D5, D10 | `send` / `write` | socket.js:204 | ABSENT | sugar; `Emit("message", ...)` equivalent | - | Y (sugar) | 0-1 | R |
| K16 | ADD V1-6 | D5, D10 | flag `volatile` (drop if transport not writable) | socket.js:39; client.js:167 | ABSENT | none; Go queue overflow closes the conn instead (namespace_conn.go:21-30, errors.go:25-33) | - | Y | 2 | R |
| K17 | ADD V1-15 | D11, D10 | `compress(bool)` | socket.js:497 | ABSENT | none; needs permessage-deflate (E8) | - | Y | 1 + E8 | R |
| K18 | - | - | `binary(bool)` | socket.js:510 | N/A | Go decides by `parser.Buffer` | - | - | 0 | R |
| K19 | - | - | flags `json`, `local` | socket.js:39-44 | N/A | `json` is a no-op; `local` only matters to a cluster adapter | - | - | 0 | R |
| K20 | ADD V1-5 | D5, D10 | `disconnect(close)`: DISCONNECT packet to the client for one namespace, or close the connection | socket.js:478-487 | PARTIAL | `Conn.Close()` closes the **whole** engine connection and all namespaces (connection.go:115-129); the server never writes a DISCONNECT packet (grep: no `parser.Disconnect` write). A Node client then sees a transport close and reconnects, instead of `io server disconnect` | `Conn.Close` | Y (`Disconnect`) | 1-2 | R |
| K21 | - | - | event `error` | socket.js:429-436 | PARITY | `OnError(ns, func(Conn, error))` server.go:142 | `OnError` | - | 0 | R |
| K22 | DONE V1-4 | D1 | client `ERROR` packet becomes socket `error` | socket.js:345 | PARITY | string, object, array and null ERROR payloads reach OnError without closing. Verified by `TestClientErrorPacket`. | - | N | 0-1 | T |
| K23 | DONE V1-4 | D1 | event `disconnecting` (rooms still joined) | socket.js:449 | PARITY | disconnecting sees joined rooms; OnDisconnect sees no rooms on peer and application close. Verified by `TestDisconnectingRooms`. | `OnDisconnect` | Y | 1 | T |
| K24 | DONE V1-4 | D1 | event `disconnect` with reason (`client namespace disconnect`, `server namespace disconnect`, `transport close`, `transport error`, `ping timeout`, `forced close`) | socket.js:421,484 | PARITY | engine causes are wrapped and mapped to namespace reasons on the read goroutine. Verified by `TestOnDisconnectReason; session.TestSessionCloseError`. | `OnDisconnect(Conn, string)` | N | 1-2 | T |
| K25 | ADD V1-5 | D5, D10 | `use(fn)` event middleware `fn(event, next)` | socket.js:548 | ABSENT | none | - | Y | 2 | R |
| K26 | ADD V1-7 | D5 | ack of client events: handler gets a callback `ack(...)`, may be called later, once only | socket.js:376-393 | PARTIAL | ack is the handler's return values, sent when it returns and when `len(ret)>0` or the client asked (connection_handlers.go:78-81). No late/async ack, no ack-callback parameter | handler return values | Y | 1-2 | R |
| K27 | - | - | server to client ack callback | socket.js:153-161,401-410 | PARITY | `Emit(..., func(args))` ack stored per conn and dropped when called (namespace_conn.go:77-90, connection_handlers.go:9-47) | `Emit` | - | 0 | R |
| K28 | ADD V1-7 | D5 | many listeners per event, `on/once/off` | EventEmitter | PARTIAL | one handler per event and namespace, set again to replace (namespace_handler.go:67-72); handlers are per namespace, not per socket | `OnEvent` | Y | 1 | R |
| K29 | - | - | decode error / protocol violation: sockets get `error`, connection closed | client.js:191-198,228-235 | PARITY | `serveRead` reports to root `OnError` and returns (server.go:389-393) | `OnError` | - | 0 | R |

## 4. Rooms / Adapter (`socket.io-adapter` 1.1.2; Redis parts: `socket.io-redis` 5.4.0)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| A1 | CHG V1-6 | D1 | `rooms`, `sids` maps | adapter 21-26 | PARTIAL | `rooms map[room]map[id]Conn` only (broadcast.go:24-28); per-conn room lookup scans all rooms (broadcast.go:155-165) | internal | N | 0-1 | R |
| A2 | ADD V1-7 | D5 | `add/addAll(id, rooms, cb)` | adapter 43-65 | PARTIAL | `Broadcast.Join(room, Conn)` takes a `Conn` not an id, one room (broadcast.go:10) | `Join` | Y | 1 | R |
| A3 | - | - | `del`, `delAll` | adapter 76-108 | PARITY | `Leave`, `LeaveAll` broadcast.go:50-75 | same | - | 0 | R |
| A4 | ADD V1-6; REDIS | D5, D2, D10 | `broadcast(packet, {rooms, except, flags})` once per socket | adapter 122-163 | PARTIAL | `Send(room)`/`SendAll` only; no except, no flags, no multi-room dedupe (broadcast.go:86-97; duplicate defect lifecycle_test.go:82-92) | `Send`, `SendAll` | Y (options) | 2 | R |
| A5 | ADD V1-6; REDIS | D5, D2, D10 | `clients(rooms, cb)` ids | adapter 173-211 | PARTIAL | `Len(room)`, `ForEach` (broadcast.go:100-126); no ids | `Len`, `ForEach` | Y | 1 | R |
| A6 | - | - | `clientRooms(id, cb)` | adapter 220 | PARITY | `Conn.Rooms()`; `Broadcast.Rooms(conn)` broadcast.go:131 | `Rooms` | - | 0 | R |
| A7 | - | - | `allRooms(cb)` **[not in socket.io-adapter 1.1.2; Redis 5.4.0 only]** | redis index.js:553 | PARITY | `Server.Rooms(ns)` server.go:299, `Broadcast.AllRooms` | `Rooms` | - | 0 | R |
| A8 | DEV | D3 | `remoteJoin`, `remoteLeave`, `remoteDisconnect`, `customRequest` **[Redis 5.4.0 only, not core adapter]** | redis index.js:598,640,680,721 | ABSENT | none | - | Y | 2-3 (needs A9 first) | R |
| A9 | DEV | D3 | Redis wire interop with `socket.io-redis` 5.4.0 (msgpack, channels `prefix#nsp#room#`, `prefix-request#nsp#`, `prefix-response#nsp#`) | redis index.js:8,85-87 | ABSENT | Go uses JSON and channel `prefix#nsp#uid` (redis_broadcast.go:171,375,609): Go instances talk to each other only (not run against Node) | `Server.Adapter` | N | 3+ | R/U |
| A10 | REDIS | D2, D8 | Redis adapter options (`key`, `host/port`, own pub/sub clients, `requestsTimeout`) | redis index.js:52 | PARTIAL | `RedisAdapterOptions{Addr, Prefix, Network, Password, DB}` adapter_options.go:6-17. **Defect**: `getOptions` never copies `DB` (adapter_options.go:34-61), so `Server.Adapter`/broadcast always use DB 0 (server.go:81, redis_broadcast.go:123-124). No TLS/user/sentinel. Not run | `RedisAdapterOptions` | N | 1 | R |

## 5. Engine.IO server options (`engine.io/lib/server.js`)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| E1 | DONE V1-2 | D15 | `pingTimeout` (20000) **D** | server.js:40 | PARITY | `TestPingTimeoutIdleClient` (ping/messages/silent): a real websocket deadline closes only after PingInterval + PingTimeout without any packet; `TestHeartbeatDefaults` asserts 20 s | `Options.PingTimeout` | N | 1 | T |
| E2 | DONE V1-2 | D15 | `pingInterval` (25000) **D** | server.js:41 | PARITY | `TestHeartbeatDefaults`: OPEN advertises PingInterval 25 s and PingTimeout 20 s | `Options.PingInterval` | N | 0 | T |
| E3 | DONE V1-2 | D5 | `upgradeTimeout` (10000) **D** | server.js:42; socket.js:189 | PARITY | `Options.UpgradeTimeout` defaults to 10 s; `TestUpgradeTimeout` closes the stalled probe within 1 s and verifies polling still works | - | Y | 1 | T |
| E4 | DONE V1-2 | D8, D1 | `maxHttpBufferSize` (1e6; also ws `maxPayload`) **D** | server.js:43,111 | PARITY (D9) | `TestMaxHTTPBufferSize`: exact-limit POST/message accepted, excessive POST is HTTP 413 and closes with transport error, excessive websocket message closes with 1009; default 1e6. POST failure uses 413 rather than destroying the HTTP connection (ROADMAP V1-2 contract) | - | Y | 1-2 | T |
| E5 | DONE V1-2 | D1 | `allowRequest(req, fn(err, ok))` **D** | server.js:46,135-167 | PARITY (D9) | RequestChecker runs for new handshakes; `TestEngineRequestValidation` pins HTTP 403 JSON with fixed Forbidden. Node reports the allowRequest error and uses plain-text 400 for an upgrade (P11; ROADMAP V1-2 contract) | `RequestChecker` | N | 1 | T |
| E6 | - | - | `transports` (order, default polling, websocket) **D** | server.js:44 | PARITY | `Options.Transports []transport.Transport`; order is the upgrade path (server_options.go:82-90, manager.go:36-45) | `Options.Transports` | - | 0 | R |
| E7 | DONE V1-2 | D5 | `allowUpgrades` **D** | server.js:45,122 | PARITY (D9) | `TestDisableUpgrades`: DisableUpgrades advertises [] and rejects an unsolicited probe with code 3; enabled sessions upgrade. Node permits an unsolicited probe (ROADMAP V1-2 contract) | `Options.Transports` | Y | 1 | T |
| E8 | ADD V1-15 | D11 | `perMessageDeflate` **D** | server.js:50,107 | ABSENT | gorilla `Upgrader` built without compression (websocket/transport.go:83-87); PROTOCOL.md lists it as "not planned" for v2 | - | Y | 1 | R |
| E9 | ADD V1-15 | D11 | `httpCompression` (gzip/deflate for polling) **D** | server.js:51; polling.js:289-320 | ABSENT | none; could be HTTP middleware | - | Y | 1 | R |
| E10 | ADD V1-15 | D11 | `cookie`, `cookiePath`, `cookieHttpOnly` (`io` cookie, sticky sessions) **D** | server.js:47-49,317-329 | ABSENT | no Set-Cookie (grep). `RequestChecker` headers could carry one but the sid is not known then | - | Y | 1 | R |
| E11 | - | - | `wsEngine` **D** | server.js:39 | N/A | selects a Node module; Go uses gorilla/websocket | - | - | 0 | R |
| E12 | - | - | `initialPacket` **D** | server.js:52; socket.js:72 | N/A | see N10 | - | - | 0 | R |
| E13 | - | - | `cors` **[not in ref: appears in engine.io 4.x]** | - | N/A | 3.6.2 has `handlePreflightRequest` + socket.io `origins` | - | - | 0 | R |
| E14 | DONE V1-2 | D1 | `attach` opts `path`, `handlePreflightRequest`, `destroyUpgrade`, `destroyUpgradeTimeout` **D** | server.js:440-499 | PARTIAL | Path is supplied by net/http; `TestEngineRequestValidation/preflight` pins code 2 for OPTIONS without sid, with no session created; existing-sid preflight remains the polling transport responsibility | - | N | 1 | T |
| E15 | DONE V1-2 | D8 | `generateId(req)` **D** | server.js:278 | PARITY | `TestRandomSessionID`: 1000 concurrent-generated, unique URL-safe ids with at least 120 random bits; the default uses crypto/rand; custom SessionIDGenerator is preserved | `SessionIDGenerator` | N (default) | 1 | T |
| E16 | - | - | `clientsCount` **D** | server.js:35 | PARITY | `TestEngineClients`: Count counts registered sessions including closed-not-removed sessions (D9); len(Clients()) gives the reference clientsCount | `Count`, `Clients` | - | 0 | T |
| E17 | DONE V1-16 | D16 | `clients` map **D** | server.js:34 | PARITY | `TestEngineClients`, `TestEngineConnState`: copied ID map includes open/closing sessions, excludes closed sessions before Remove | `Clients` | Y | 1 | T |
| E18 | - | - | `connection` event **D** | server.js:342 | PARITY | pull model `engineio.Server.Accept()` engineio/server.go:65 | `Accept` | - | 0 | R |
| E19 | DONE V1-17 | D16 | engine events `flush`, `drain`, socket `packet`, `packetCreate`, `heartbeat`, `upgrading`, `upgrade`, `error` **D** | socket.js | PARITY | `TestEngineSocketEvents`: polling batch, heartbeat, packet types, upgrade ordering and transport Error before close; `TestEngineServerEvents`: connection, initial headers, all response headers and rejection codes | `Options.Hooks` | Y | 1-2 | T |
| E20 | DONE V1-16 | D16, D10 | engine socket `close(discard)`, `send`, `readyState`, `upgraded`, `transport` **D** | socket.js:364,462 | PARITY | `TestEngineConnState`, `TestEngineClientState`, `TestSessionReadyState`: state around Close, transport and upgrade at the switch, client state after Close and ping-loop exit | `ReadyState`, `Transport`, `Upgraded`, `Close`, `NextWriter` | Y | 1 | T |
| E21 | - | - | `handleRequest`, `handleUpgrade` **D** | server.js:214,351 | PARITY | `ServeHTTP` handles both (engineio/server.go:104) | `ServeHTTP` | - | 0 | R |
| E22 | DONE V1-2 | D8 | `close()` closes all clients **D** | server.js:191 | PARITY | `TestServerCloseClosesAcceptedSessions` and `TestServerCloseClosesUnacceptedSessions`: accepted, queued and opening sessions are closed/removed; new handshakes after Close fail | `Close` | N | 1 | T |
| E23 | - | - | `EIO` query parameter check | server.js (none) | PARITY | neither checks it in 3.x (PROTOCOL.md) | - | - | 0 | R |

## 6. Engine.IO protocol v3 behaviour (`engine.io`, `engine.io-parser` 2.2.1)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| P1 | - | - | OPEN packet `sid/upgrades/pingInterval/pingTimeout` | socket.js:65 | PARITY | parameters.go:17-52; session.go InitSession | - | - | 0 | R |
| P2 | - | - | ping direction v3: client `ping`, server `pong` | socket.js:98-102 | PARITY | session.go:193-200 | - | - | 0 | R |
| P3 | DONE V1-2 | D1 | any packet resets the liveness timer | socket.js:93-95 | PARITY | `TestPingTimeoutIdleClient/messages`: only MESSAGE every 100 ms keeps a 150 ms liveness window open beyond 500 ms; stopping packets closes with ping timeout | - | N | 0-1 | T |
| P4 | DONE V1-2 | D1, D9 | upgrade probe `2probe`/`3probe`/`5`: pause polling on `5`, noop to the pending poll at a 100 ms check, polling kept on failure | socket.js:180-266 | PARITY | `TestUpgradeProbeWindow` (post/cadence/failed/timeout): POSTs work after 3probe, each packet arrives once, NOOP checks are 100 ms apart, only 5 pauses polling; failed/timed-out probes leave polling working | - | Y | 0-1 | T |
| P5 | DONE V1-1 | D4 | JSONP GET `___eio[<j>]("...")`, `j` reduced to digits | polling-jsonp.js:24,56-74 | ABSENT | removed by V1-1 (D4): `j` is ignored, a poll answers the ordinary payload and headers, nothing of `j` is written (`TestPollingIgnoresJSONPCallback`). Audit: `___eio[` + raw `j` + `]("`, `j` not sanitised | - | N | 1 | T |
| P6 | DONE V1-1 | D4 | JSONP POST (`application/x-www-form-urlencoded`, field `d`, escaped `\n`) | polling-jsonp.js:39-54 | ABSENT | removed with JSONP (D4): POST accepts only `application/octet-stream` or `text/plain; charset=utf-8` (polling/util.go:21-40); a form POST, with or without `j`, is HTTP 400 and reaches no session (`TestPollingRejectsJSONPFormPost`) | - | N | 1 | T |
| P7 | - | - | `b64=1` base64 text payload; `supportsBinary` otherwise | server.js:304-308; polling.js | PARITY | polling/server.go:31,126-131; payload/encoder.go b64 writer | - | - | 0 | R |
| P8 | CHG V1-18 | D15 | XHR CORS headers on every polling response | polling-xhr.js:48-62 | PARTIAL | only if `CheckOrigin` is set; default nil sends none (polling/server.go:70-85, transport.go:19-24) | `CheckOrigin` | N | 1 (with S4) | R |
| P9 | TEST V1-8 | D7 | OPTIONS answer with `Access-Control-Allow-Headers: Content-Type` | polling-xhr.js:32-41 | PARITY | polling/server.go:101-107 (see E14 for the no-sid case) | - | - | 0 | R/U |
| P10 | - | - | IE `X-XSS-Protection: 0` | polling.js:398-403 | PARITY | polling/server.go:65-67 | - | - | 0 | R |
| P11 | DONE V1-2 | D1, D9 | error replies JSON `{code,message}`, codes 0-4, CORS headers on 400, 403 for rejected, to a request that is not a websocket upgrade; a rejected upgrade gets `abortConnection`'s plain-text 400 with the code's message | server.js:72-86,243-268,351-358,509-526 | PARTIAL (D9) | `TestEngineRequestValidation`: JSON codes 0–4, HTTP 400/403; a rejected websocket upgrade gets the same JSON/status and a checker rejection always says Forbidden, unlike Node plain-text upgrade errors and allowRequest error messages (ROADMAP V1-2 contract); CORS headers are V1-18 | - | N | 1 | T |
| P12 | DONE V1-2 | D1 | handshake must be GET (`BAD_HANDSHAKE_METHOD`) | server.js:163-164 | PARITY | `TestEngineRequestValidation/post` and `/preflight`: a handshake without sid requires GET, otherwise HTTP 400 with code 2 Bad handshake method and no session | - | N | 0-1 | T |
| P13 | - | - | unknown transport / unknown sid / transport changed without upgrade give 400 | server.js:136-160 | PARITY | engineio/server.go:107-112,126-134,157-162 (`canUpgrade`) | - | - | 0 | R |
| P14 | - | - | Origin header with invalid chars rejected | server.js:143-149,561 | N/A | `net/http` rejects invalid header values at parse time | - | - | 0 | U |
| P15 | TEST V1-8 | D7 | websocket: one packet per frame, binary frame carries raw bytes, text for strings | websocket.js:76-103 | PARITY | websocket/wrapper.go; PROTOCOL.md (not re-verified in depth) | - | - | 0 | R/U |
| P16 | DONE V1-1 | D8, D7 | polling payload `<len>:<packet>` in UTF-16 code units, `b`+base64 for binary | engine.io-parser | PARITY | text payload (`b64=1`) in UTF-16 code units; the binary payload counted UTF-16 units too, where engine.io-parser 2.2.1 counts bytes, so non-ASCII text failed for a client polling without `b64=1` (the browser build of socket.io-client 2.x with XHR2; reproduced with engine.io-parser 2.2.1, not with a browser; in Node, engine.io-client sends `b64=1` and was not affected). V1-1 counts bytes (payload/encoder.go `writeBinaryHeader`, decoder.go `Read`); `TestPollingBinaryPayloadNonASCII` and the payload vectors use bytes produced by engine.io-parser 2.2.1 | - | - | 0 | T |
| P17 | TEST V1-8 | D7 | graceful close: flush buffered packets, `close` packet on polling | socket.js:462-485; polling.js:356-385 | PARITY | `Conn.Close` drain with deadline (connection.go:115-129) | - | - | 0 | R/U |
| P18 | TEST V1-8 | D7 | overlapping poll / data requests rejected (`overlap from client`) | polling.js:81-88,126-132 | PARITY (unverified) | Go payload has `flushing`/`feeding` guards (payload/payload.go:29,35,97); behaviour not run | - | - | 0 | U |

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

## 8. Client (`socket.io-client` 2.5.0 / `engine.io-client` 3.5.6 vs `client.go`)

| ID | Plan | Decision | Reference item | Ref | Status | Go evidence | Nearest Go API | API? | PRs | Ver |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| C1 | - | - | `io(url, opts)` / `Manager` / `socket(nsp)` | client lib/index.js:36; manager.js:370 | PARTIAL | `NewClient(addr, *engineio.Options)` client.go:41; one `Client` = one connection = one namespace (client.go:25-37) | `NewClient` | - | 0 | R |
| C2 | ADD V1-10 | D6 | multiplexing namespaces on one connection | manager.js:370 | ABSENT | none | - | Y | 2 | R |
| C3 | ADD V1-11 | D6, D12 | reconnection (options + events `reconnect*`) | manager.js:127-190,519-580 | ABSENT | none | - | Y | 2 | R |
| C4 | ADD V1-10 | D6 | `timeout`, `connect_error`, `connect_timeout` | manager.js:191,221-290 | ABSENT | `Connect()` returns an error (client.go:78-103), no timeout option | - | Y | 1 | R |
| C5 | ADD V1-10 | D6 | websocket transport and upgrade | engine.io-client | ABSENT | `Connect` dials polling only (client.go:79-81) although `websocket.Transport.Dial` exists | - | Y | 1 | R |
| C6 | ADD V1-10 | D6 | `query`, `path`, `extraHeaders` options | manager.js:44 | PARTIAL | query kept from URL (client.go:46-60); path fixed `/socket.io`; headers `nil` (client.go:85) | - | Y | 1 | R |
| C7 | - | - | `emit` with ack, `on(event)`, `connect/disconnect/error` events | socket.js:136 | PARITY | `Client.Emit`, `OnEvent`, `OnConnect`, `OnDisconnect`, `OnError` client.go:111-162; single handler per event | - | - | 0 | R |
| C8 | ADD V1-11 | D6, D10 | `once/off`, `ping/pong` events, `socket.id/connected` | socket.js | ABSENT | none | - | Y | 1 | R |
| C9 | CHG V1-10 | D6 | emit before connect is buffered | socket.js:345 | ABSENT | logged and dropped (client.go:113-119) | - | N | 1 | R |
| C10 | CHG V1-10 | D6 | client sends DISCONNECT on close | socket.js:398-420 | ABSENT | `Client.Close` closes the conn only (client.go:107, connection.go:115) | - | N | 1 | R |
| C11 | ADD V1-11; ADD V1-15 | D6, D11, D10 | flags `volatile`, `compress`, `binary` | socket.js:422-440 | ABSENT | none; the 2.5.0 client defines `compress` (socket.js:422-424) and `binary` (:435-436) only, no `volatile`, so the client `Volatile` is the D10 method of V1-11 (a Go extension, not a reference behaviour) | - | Y | see K | R |

## Go-only (implemented-only) surface, not in the reference

`engineio.Options.Logger`, `WriteBufferSize` with `ErrWriteBufferFull` (bounded per-conn queue
that closes the conn on overflow, errors.go:25-33), `ConnInitor`, `Server.Remove(sid)`,
`Server.ClearRoom`, `Server.RoomLen`, Redis-backed room queries, `Conn.Context/SetContext`,
`parser.Buffer`. The bounded queue closes a healthy slow client where Node would buffer
without limit; it is a behaviour difference with no reference counterpart.

## Notable findings outside the matrix rows

1. Default session id is a sequential counter (E15): ids are guessable.
2. Polling JSONP callback `j` is echoed unsanitised (P5).
3. No cap on attachment count (R6, measured) and no payload size cap (E4).
4. `RedisAdapterOptions.DB` is dropped by `getOptions` (A10).
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
