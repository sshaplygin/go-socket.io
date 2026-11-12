# V1 protocol support

This module implements Socket.IO protocol v4 over Engine.IO protocol v3.
Supported clients are listed in [README.md](../README.md#compatibility).

## Engine.IO protocol v3

Package `engineio` of the v1 module at the repository root.

- Query parameter `EIO` is not checked; the server behaves as protocol v3 regardless.
- Transports: `polling` (XHR only; JSONP is unsupported and the `j` query parameter is
  ignored) and `websocket` (`gorilla/websocket`). Order and upgrade path come from `engineio.WithTransports`,
  default `[polling, websocket]`.
- Handshake: a GET without `sid` opens a session (any other method is answered HTTP 400
  with code 2, below). The `OPEN` packet carries `sid` (random, as engine.io's
  `base64id`), `upgrades` (`[]` with `WithUpgrades(false)`), `pingInterval` (default
  25 s) and `pingTimeout` (default 20 s). There is no `maxPayload`: protocol v3 has none.
  The JSON of the `OPEN` packet ends with a newline (`\n`), an intentional
  whitespace difference from engine.io 3.6.2.
- Heartbeat: the client sends `PING` (`2`), the server answers `PONG` (`3`). Every packet
  the session reads re-arms a window of `pingInterval + pingTimeout`; when it passes
  without one, the session closes with reason `ping timeout`.
- Polling payload: with `b64=1` packets are length-prefixed (`<length>:<packet>`),
  lengths count UTF-16 code units, and binary packets are base64 with a `b` prefix.
  Otherwise the payload is binary: per packet a `0` (string) or `1` (binary) byte, the
  length as one byte per decimal digit, `0xff`, then the packet, the length counting
  bytes (UTF-8 for a string packet), as engine.io-parser 2.x. Sessions are looked up
  by `sid`; an unknown `sid` is HTTP 400. Server GET responses coalesce complete
  packets for up to 1 ms idle or 5 ms total, at most 64 packets or 256 KiB checked
  between packets. A single packet can exceed that batching byte budget. A change
  in compression preference starts the next response; cancellation fails the transport.
- WebSocket: one packet per message; a binary message starts with the packet type byte.
  The optional permessage-deflate extension is off by default, with a 1024-byte
  encoded-packet threshold when enabled. The Go client offers it when configured,
  with the same threshold. Per-packet compression defaults to true; false suppresses
  it for the packet and all its binary attachments on either transport.
  With `b64=1` in the websocket request, as sent by a `forceBase64` client, the
  server writes binary packets as text messages `b<type><base64 data>` and decodes
  such text messages as binary packets, both on a direct websocket connection and
  after a polling-to-websocket upgrade. Decoding is strict: unpadded data or invalid
  characters close the session; the reference clients always pad. Binary messages
  are still accepted in this mode; socket.io-client 1.7.4 under Node sends them
  despite `forceBase64`. Without `b64`, a text message starting with `b` is an
  invalid packet type and closes the session; engine.io 3.6.2 decodes it as base64
  in either mode.
  A server-initiated websocket close, such as `Conn.Close`, `Server.Close`, a
  client `CLOSE` packet or a ping timeout, sends a close frame with code 1000 before
  the TCP connection is closed, as engine.io 3.6.2 does, so the client observes close
  code 1000. The server then waits up to 250 ms for the client's closing reply, so
  that the TCP close does not reset it (ws 1.1.5, used by socket.io-client 1.7.4 under
  Node, reports a reset as 1006 even after it read the frame), and closes the TCP
  connection. The frame is written with a 500 ms deadline: a peer that has stopped
  reading or does not reply delays the close by at most that long and then sees the
  connection drop (1006). `engineio.Abort`, and a session that failed with a transport
  error, drop the connection without a frame, so the client observes 1006; so can a
  connection that resets before the client reads the frame, for example when the
  server still has unread client data. An upgrade probe the session has not
  switched to is not an established connection and is the exception: a session
  closed while it is a probe drops it without a frame (1006), see the upgrade
  bullet below. The message-limit close below sends code 1009. The reference
  clients report the close frame as `transport close`, and so does the Go client for
  codes 1000 and 1001 and for a frame without a code, which engine.io 3.6.2 sends;
  other codes, such as 1009 or 1011, remain `transport error`. The frame is sent by
  server connections only: the Go client closes its websocket without one.
- A websocket close frame from the client with code 1000 or 1001, or without a code
  (1005, which socket.io-client 2.5.0 sends when its engine closes), ends the session
  with `transport close`, as a `CLOSE` packet does. Any other code, such as 1008 or
  1011, and a connection dropped without a frame (1006) end it with
  `transport error`. engine.io 3.6.2 reports every websocket close as
  `transport close`, whatever its code and also for a dropped connection; this server
  keeps `transport error` for those, so that a failed or crashed peer stays distinct
  from an orderly close.
- Polling HTTP compression is enabled by default: gzip or zlib deflate according
  to Accept-Encoding, gzip preferred at equal quality, above the 1024-byte response
  threshold and only for a packet whose compression flag is true. The threshold is
  configurable; responses below it remain uncompressed. A batch shares one compression
  stream and contains only packets with the same compression preference.
- The polling transport that opens a session sets its session cookie on every
  response (default io, Path=/, HttpOnly, SameSite=Strict). The cookie's attributes
  and name are configurable, and it can be disabled. A websocket-first handshake
  sets no cookie, matching engine.io 3.6.2.
- Message limit: `WithMaxHTTPBufferSize`, default 1e6 bytes. A longer polling POST
  body is answered HTTP 413 (plain text) and closes the session; a longer websocket
  message closes the connection with close code 1009. A client can see 1006 instead
  when the connection resets before it reads the close frame. With permessage-deflate,
  the same limit applies to inflated bytes; rejection does not buffer the full message.
- The Go client limits incoming websocket messages (inflated bytes) and whole decoded
  polling response bodies, OPEN included, to 1 MiB by default. The configurable
  limit also applies to reconnects and candidate upgrade transports before probe I/O.
- When the Go client closes a polling connection, it queues `CLOSE` (`1`) behind the
  packets already written, lets the in-flight POST finish and sends `CLOSE` in a final
  POST before it cancels outstanding requests, as engine.io-client 3.5.6 does. `Close`
  does not wait for it; the in-flight and final POSTs share a 2 s bound, and a failed
  close POST is not reported. No `CLOSE` is sent after the client has recorded a
  transport failure, after it has read the server's `CLOSE`, or after it has switched
  to an upgraded websocket, which is closed directly. A `Close` that runs before the
  client has seen one of these still attempts the packet: the POST is rejected with
  HTTP 400 (after the switch, or an unread server `CLOSE` whose session is gone), or
  the write fails on the failed transport and nothing is sent. Neither is reported.
- Duplicate websocket requests carrying a live websocket session ID receive HTTP
  400/code 3. This immediate rejection is Go-specific; the Node reference does not
  use that response for this case.
- Upgrade polling → websocket: the client's `2probe` over the websocket is answered
  `3probe`. Until the client's `UPGRADE` (`5`), POSTs are accepted and their packets
  delivered, and a pending poll is answered with data or, at a 100 ms check, a NOOP;
  the polling connection is paused only on `5`, then the session switches to the
  websocket and closes the polling connection. A failed probe, or one without `5`
  within `WithUpgradeTimeout`, closes the probe connection and leaves polling
  unpaused; that close of an expired or failed probe, made while the session stays
  open, is an ordinary websocket close with the 1000 frame described above. A session
  closed while a probe is in progress (`Conn.Close`, `Server.Close`, `Abort`) drops
  the probe connection at once, from the moment the probe is accepted until the
  server switches to it after the client's `5`, without the close frame and without
  waiting for a reply that an established websocket receives, so the probing client
  observes 1006 on it. From the switch on, the websocket is the session's connection
  and closes as described above (1000 for `Conn.Close` and `Server.Close`), also
  while the server finishes the upgrade by closing the polling connection. When the
  expiry of a probe and the close of the session coincide, the client observes
  whichever came first: 1000 from the expiry, 1006 from the close. Implemented in the private server session upgrade state machine (`engineio/internal/session/upgrade.go`).
- Requests the server rejects before a transport handles them are answered with JSON
  `{"code":<n>,"message":"<text>"}`, as engine.io 3.6.2 answers a request that is not a
  websocket upgrade: HTTP 400 with code 0 `Transport unknown`, 1 `Session ID unknown`, 2
  `Bad handshake method` or 3 `Bad request` (also a transport that cannot accept the
  request, except a websocket handshake that gorilla/websocket answers itself, and a
  session that cannot be created), and HTTP 403 with code 4 and the fixed message
  `Forbidden` when `WithRequestChecker` rejects the request (3.6.2 sends the error
  of `allowRequest` as the message). A websocket upgrade request gets the same
  answers, where engine.io 3.6.2 writes a plain-text HTTP 400 and closes the socket. What
  the polling transport rejects itself stays plain text: HTTP 400 for a POST the payload
  cannot take (paused, overlapping, closed, timed out or not decodable) or an invalid
  method, 413 for the message limit above, 500 for a poll
  whose response failed before it was started. A POST whose body read the session's
  close interrupted is answered with `Connection: close`.
  A polling POST body is decoded by its `Content-Type`, as in engine.io 3.6.2:
  `application/octet-stream` selects the binary payload and every other value, a
  missing header, `text/plain` with or without a charset, `application/x-www-form-urlencoded`
  (what `curl -d` sends) and a value that is not a valid media type included, selects the
  text payload. The charset is not checked. The check on the media type is a Go
  difference from the reference: it ignores case and parameters, where 3.6.2 compares
  the whole header value with `application/octet-stream`, so
  `Application/Octet-Stream` and `application/octet-stream; charset=binary` are binary
  here and text there. The type never depends on the session's `b64` parameter, and the
  body is not sniffed: a binary payload sent as text, or a text payload sent as
  `application/octet-stream`, is not decodable and is answered HTTP 400.
  The request body limit and the other refusals apply whatever the header says.

### CORS

By default both built-in transports accept every origin. Polling reflects the
request's `Origin` in `Access-Control-Allow-Origin` and sets
`Access-Control-Allow-Credentials: true`; without an Origin it uses `*` and omits
credentials. Websocket accepts foreign origins for direct connections and upgrades.
An `OPTIONS` request on an open polling session returns HTTP 200 with
`Access-Control-Allow-Headers: Content-Type` and the same origin headers. The
transport always sets that value, replacing an `Access-Control-Allow-Headers`
header set by middleware in front of the handler. An
`OPTIONS` request without a `sid`, such as the preflight of a handshake that sends
custom headers (for example `extraHeaders`), is answered HTTP 400 with code 2
`Bad handshake method` (see the rejection responses under
[Engine.IO protocol v3](#engineio-protocol-v3)).

`engineio.WithCORS(false)` removes `Access-Control-Allow-Origin` and
`Access-Control-Allow-Credentials` from polling responses, including error responses.
An open session's `OPTIONS` request still returns HTTP 200 and
`Access-Control-Allow-Headers: Content-Type`. Cross-origin polling requests are
still served: CORS headers control browser access to responses, not HTTP access.
Websocket instead uses its same-origin check and rejects foreign origins with
HTTP 403. Requests without an Origin remain accepted.

Explicit transport `CheckOrigin` callbacks override either default for requests
dispatched to that transport. An error response for an unknown `transport=` value
still uses the server-wide `WithCORS` setting. For example, with imports
`net/http`, `engineio`, `engineio/transport/polling` and
`engineio/transport/websocket` from this module, an application can configure:

```go
allowOrigin := func(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin == "" || origin == "https://chat.example.com"
}
server := engineio.NewServer(engineio.WithTransports(
	&polling.Transport{CheckOrigin: allowOrigin},
	&websocket.Transport{CheckOrigin: allowOrigin},
))
```

Browsers always send `Origin` on websocket handshakes, so accepting an empty
Origin keeps cross-site WebSocket hijacking protection while allowing non-browser
clients, such as this module's Go client and Node clients, which send none.

A false polling origin check suppresses CORS headers; a false websocket check
rejects the handshake. Applications needing to reject HTTP requests must also
use request authentication/access controls, such as HTTP middleware. In
particular, do not rely on the permissive default for cookie-authenticated access.

## Socket.IO protocol v4

The v1 module at the repository root: the `socketio` package and `parser` (`parser.Buffer`,
`Header.Query`). It runs over the root `engineio` package of the previous section.

- Packet format `<type>[<attachments>-][<namespace>,][<ack id>][JSON]`. Types
  0 CONNECT, 1 DISCONNECT, 2 EVENT, 3 ACK, 4 ERROR, 5 BINARY_EVENT, 6 BINARY_ACK.
  An unknown or invalid packet type closes the connection.
  An encoded JSON payload ends with a newline (`\n`), for example
  `2["hello","x"]\n` (Engine.IO message `42["hello","x"]\n`); a packet without a
  payload, such as the CONNECT packet `0`, has none. This is an intentional
  whitespace difference from the reference server.
- On a new session the server connects the client to the root namespace `/`
  automatically and sends `0` before any client packet. Other namespaces are joined
  when the client sends `0/<nsp>`.
- A CONNECT to a namespace without a registered handler is answered with an ERROR packet
  (`4/<nsp>,"Invalid namespace"`) and the connection stays open.
- Binary attachments are replaced by `{"_placeholder":true,"num":N}` and sent as
  following binary frames; on the Go side they map to `parser.Buffer`.
- Event handlers return values become the ACK payload; an ACK packet is sent when the
  client requested an ack and a listener handled the event. Emitting with a trailing
  function argument registers an ack callback. When an event's arguments do not
  decode into an untyped handler's parameter types, for example a string for an
  `int`, the handler does not run and the connection stays open. This holds for
  the server and for the Go client alike. If the peer requested an ack, the
  receiving side answers with the error ACK of acknowledged typed descriptors
  (`NewAckEvent`), `3<id>[{"code":"invalid_payload","message":"decode argument 0:
  json: cannot unmarshal string into Go value of type int"}]`, so a JavaScript
  callback, or the callback of a Go `Emit` with an acknowledgement, is called
  with that object as its only argument, and `EmitWithAck` returns a
  `*RemoteError` with code `invalid_payload`; without an ACK id nothing is sent. The decode error goes to `OnError` in both cases. This
  covers a wrong JSON type and a binary attachment where another type is
  expected. A missing argument is zero-filled and extra arguments are ignored, so
  the handler runs and its return value is the ACK. A packet that is not valid
  (an event payload that is not an array) or an invalid attachment closes the
  connection and sends no ACK. The reference server has no equivalent, because its
  handlers accept any arguments.
- An ERROR payload is always a JSON string: `err.Error()` for a connect or event
  middleware rejection or an `OnConnect` error, for example `4"rejected"`, and
  `Invalid namespace` for an unknown namespace. Additional error data is not
  sent, even when the error value implements `json.Marshaler`. As a known
  compatibility limitation, socket.io 2.5.0 instead sends `err.data` when it is
  set, which can be any JSON value, and `err.message` otherwise.
- Namespace query strings (`0/nsp?x=1`) are parsed into `Header.Query` and merged into
  `Handshake().Query` of that socket.
- The reference server queues outbound packets without a bound. This server bounds
  the queue per connection with `WithWriteBufferSize` (1024 packets by default), so a
  burst that must be queued at once, such as one handler's emits over polling, has to
  fit the bound. `ErrWriteBufferFull` documents the overflow behavior.

A Socket.IO packet may contain at most 10 binary attachments. Encoding counts
only attachments referenced by the encoded JSON and rejects a larger packet
before writing any frames; decoding rejects a header declaring more than 10
attachments. JSON field selection applies to embedded and private fields.
Except for `parser.Buffer`, values implementing JSON or text marshaling use
their marshaled representation; their fields are not inspected for binary
attachments.

## Redis cluster interoperability

The v1 Redis adapter speaks the broadcast and request protocol of
`socket.io-redis` 5.4.0 with `socket.io` 2.5.0: MessagePack event envelopes with
binary values, room unions, socket exclusions and delivery flags; JSON cluster
queries and remote join/leave/disconnect/custom requests. Go and Node instances
can share a standalone Redis Pub/Sub cluster. Mixed-peer namespace names must be
preserved by Node's URL-path parsing and must not contain `#`. Node 2.5 CONNECT
parsing strips URL fragments, and Node Redis 5.4.0 prefix-matches request channels:
a request for `/cluster#other` can execute against `/cluster`. Such names are
unsupported in mixed clusters, including administrative requests; Go cannot fix
routing performed by an unmodified Node peer.

Go-only adapters route requests by exact channel and namespace. Broadcast namespace
identity is checked from the packet independently of channel separators, including
`#`; that broadcast check does not repair Node's request routing. Custom payloads are JSON,
not binary, matching the reference adapter.

Redis broadcast envelopes must contain one complete MessagePack value. Declared
lengths must fit the message, and nesting is limited to 1000 levels before decoding.
Malformed envelopes are discarded.

Redis Pub/Sub provides no replay during disconnection. Queries are bounded,
non-atomic snapshots; context-aware operations report timeouts and partial results.

### Upgrading from upstream Go Redis adapters

The Redis adapter is wire-incompatible with `googollee/go-socket.io` v1.7.0.
Although both use `socket.io` as their default prefix, their channel names and
broadcast codecs differ. The `%s` placeholders below are, in order, the prefix, the
namespace and (in the upstream key) the instance ID:

| Item | Upstream Go v1.7.0 | This fork |
| --- | --- | --- |
| Subscription pattern | `%s#%s#*` | `%s#*` |
| Key | `%s#%s#%s` | `%s#%s#` |
| Request channel | `%s-request#%s` | `%s-request#%s#` |
| Broadcast codec | JSON | MessagePack |

Do not run old and new Go instances with the same prefix during an upgrade.
For a shared non-root namespace, any fork broadcast published on the namespace
channel, to the whole namespace or to more than one room, stops every upstream
peer's Redis dispatcher in that namespace on a decode error. A single-room
broadcast does the same when the room name, or its last `#`-separated segment,
equals the namespace name. Those peers then receive no broadcasts and answer no
cluster requests; their `Server.RoomLen` and `Server.Rooms` calls block without a
timeout.
The fork discards upstream messages it cannot decode.

Root-namespace broadcasts are lost in both directions but do not stop upstream
dispatchers. This fork normalizes the root namespace to `/`: its root channels
(`socket.io#/#…`) do not match upstream's root subscription (`socket.io##*`),
which uses an empty namespace. Upstream root broadcasts (`socket.io##<uid>`) do not
match the fork's root channel prefix (`socket.io#/#`), so the fork ignores them.

For a staged upgrade, give every new instance a distinct prefix, for example
`redisadapter.New(redisadapter.WithPrefix("socket.io-fork"))`. Old and new fleets
are then separate clusters: broadcasts and room queries do not cross between
them. Move clients to the new fleet and retire the old instances before reusing
the original prefix. Redis Pub/Sub is shared across database numbers, so changing the
Redis database alone does not isolate fleets. Alternatively, stop all old
instances before starting the new fleet with the original prefix; upgrade every
instance together without an overlap.

## JSONP

The v1 Engine.IO transport supports XHR polling and websocket, without JSONP.
The `j` query parameter is ignored and the response is an ordinary polling
payload, not a JavaScript callback. A JSONP form POST (`d=`) is rejected with
HTTP 400. Clients must use XHR polling or websocket.

## Admin UI

The official `@socket.io/admin-ui` requires Socket.IO protocol v5 over Engine.IO v4.
The v1 module implements Socket.IO v4 over Engine.IO v3 and cannot host it.
