# Engine.IO v4 WebSocket packet fixtures

The staged codec handles the contents of complete WebSocket data messages.
`packets.json` covers all Engine.IO packet types, upgrade probes, Unicode, text
containing the polling separator, empty messages, raw binary and base64 fallback.
Fixtures are authored wire examples checked independently by Go and the pinned
Node `engine.io-parser@5.2.3`, with binary support both enabled and disabled.

References: the [WebSocket packet format](https://socket.io/docs/v4/engine-io-protocol/#websocket-1),
the pinned [TS encoder](https://github.com/socketio/socket.io/blob/engine.io-parser%405.2.3/packages/engine.io-parser/lib/encodePacket.ts)
and [TS decoder](https://github.com/socketio/socket.io/blob/engine.io-parser%405.2.3/packages/engine.io-parser/lib/decodePacket.ts).
Raw binary has no Engine.IO type byte in v4; every byte belongs to MESSAGE data.
Text fallback uses `b` plus base64 and decodes back to a binary MESSAGE. Text has
no polling separator restriction because WebSocket supplies message boundaries.

From the repository root:

```sh
go test -race -count=1 -cover ./engineio/transport/websocket/internal/eio4
npm ci --ignore-scripts --no-audit --no-fund --prefix engineio/transport/websocket/internal/eio4/testdata/reference
npm test --prefix engineio/transport/websocket/internal/eio4/testdata/reference
go test ./engineio/transport/websocket/internal/eio4 -run '^$' -fuzz '^FuzzDecode$' -fuzztime 20s -parallel 2
go test ./engineio/transport/websocket/internal/eio4 -run '^$' -fuzz '^FuzzBinaryRoundTrip$' -fuzztime 20s -parallel 2
```

Node is optional test tooling; Go tests need neither Node nor external services.
The dependency and integrity hash are fixed in `reference/package-lock.json`.
Malformed-input, byte-limit and ownership checks live in the Go tests. As with
the staged polling codec, Go rejects invalid UTF-8 and noncanonical base64;
this does not claim identical error handling to JS. Go also rejects binary
control packets because that wire representation cannot preserve their type.

This is preparation for the transport rewrite after roadmap stage 1b. It does
not replace gorilla, perform an HTTP upgrade or implement RFC 6455 framing,
fragment reassembly or control-frame handling. The transport must enforce its
read limit before buffering a complete message. The current v1 connection still
uses its existing packet encoder and decoder.
