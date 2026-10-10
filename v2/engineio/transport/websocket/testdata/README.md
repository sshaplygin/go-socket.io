# Engine.IO v4 WebSocket fixtures and Node oracles

The codec handles the contents of complete WebSocket data messages.
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

From `v2/`, the directory of the v2 module (at the repository root these paths name
the v1 packages):

```sh
go test -race -count=1 -cover ./engineio/transport/websocket
npm ci --ignore-scripts --no-audit --no-fund --prefix engineio/transport/websocket/testdata/reference
npm test --prefix engineio/transport/websocket/testdata/reference
REQUIRE_NODE_ORACLE=1 go test -count=1 -run '^TestNodeOracle$' -v ./engineio/transport/websocket
go test ./engineio/transport/websocket -run '^$' -fuzz '^FuzzDecode$' -fuzztime 20s -parallel 2
go test ./engineio/transport/websocket -run '^$' -fuzz '^FuzzBinaryRoundTrip$' -fuzztime 20s -parallel 2
go test ./engineio/transport/websocket -run '^$' -fuzz '^FuzzReadMessage$' -fuzztime 20s -parallel 2
```

Node is optional test tooling; the Go tests need neither Node nor external services, and
`TestNodeOracle` is skipped without the installed dependencies unless
`REQUIRE_NODE_ORACLE` is set. The dependencies and integrity hashes are fixed in
`reference/package-lock.json`: `engine.io-parser@5.2.3` (`verify.cjs`, the packet
fixtures above) and `ws@8.18.3` (`verify-framing.mjs`).

`verify-framing.mjs` is the independent peer of the transport itself. `TestNodeOracle`
serves an echo of `Transport` with a 64-byte `MaxPayload` and the script drives it with
`ws`: text and binary packets, the `b` + base64 form, empty binary messages, a text
message fragmented inside a UTF-8 sequence with a ping between the fragments, a normal
close, and eight violations, each of which must end with the RFC 6455 close status
(1009 for an oversized frame or fragmented message, 1007 for invalid UTF-8 also across
fragments, 1002 for an unmasked client frame, an empty text message, an unknown packet
type and malformed base64), never a bare TCP close. `wsutil.ControlFrameHandler`
answers a close frame with the status and without the reason, where Node `ws` echoes
both; the script asserts this.

The fixtures cover the codec (`codec.go`); framing, fragments, control frames, limits and
close statuses are tested in `message_test.go`, `transport_test.go` and `fuzz_test.go`.
Malformed-input, byte-limit and ownership checks live in the Go tests. As with the polling
codec, Go rejects invalid UTF-8 and noncanonical base64; this does not claim identical error
handling to JS. Go also rejects binary control packets because that wire representation
cannot preserve their type.
