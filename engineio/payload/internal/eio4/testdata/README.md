# Engine.IO v4 polling fixtures

`payloads.json` is shared by the Go codec tests and the optional Node reference
check. `spec-text` and `spec-binary` reproduce the two polling encoding examples
from the [official protocol](https://socket.io/docs/v4/engine-io-protocol/#http-long-polling-1).
The remaining fixtures are authored edge cases: all packet types, an open packet,
upgrade probe data, Unicode, control characters allowed in text, empty messages,
empty binary data, and every base64 padding length. The open packet's JSON is
opaque to this codec; session-level validation belongs to the transport rewrite.

`body-limits.json` records accepted and oversized polling bodies. Go exercises
`DecodeReader` with one-byte reads; the Node check sends each case over HTTP to
`engine.io@6.6.4`, with both Content-Length and chunked requests (including splits
inside UTF-8 characters). It checks status 200/413, delivered message contents,
and that oversized bodies dispatch nothing. These are fixture tests of the Node
server and the isolated Go reader, not a claim of Go server interoperability.

From the repository root:

```sh
go test -race -count=1 -cover ./engineio/payload/internal/eio4
npm ci --ignore-scripts --no-audit --no-fund --prefix engineio/payload/internal/eio4/testdata/reference
npm test --prefix engineio/payload/internal/eio4/testdata/reference
go test ./engineio/payload/internal/eio4 -run '^$' -fuzz '^FuzzDecode$' -fuzztime 20s -parallel 2
go test ./engineio/payload/internal/eio4 -run '^$' -fuzz '^FuzzBinaryRoundTrip$' -fuzztime 20s -parallel 2
go test ./engineio/payload/internal/eio4 -run '^$' -fuzz '^FuzzDecodeReader$' -fuzztime 20s -parallel 2
go test ./engineio/payload/internal/eio4 -run '^$' -bench BenchmarkDecode -benchmem
```

The reference dependencies are pinned to `engine.io-parser@5.2.3` (protocol 4) and
`engine.io@6.6.4`, including npm integrity hashes. The optional checks need Node
18+ and loopback HTTP access. They are test tooling only: ordinary Go tests and
the root module require neither Node nor npm. Both codec implementations independently
encode packets to the expected wire body and decode that body to expected packets.

The read-limit behavior was checked against the pinned
[TS polling transport](https://github.com/socketio/socket.io/blob/engine.io%406.6.4/packages/engine.io/lib/transports/polling.ts):
it checks incoming body bytes against `maxHttpBufferSize`, returns 413 on overflow,
and decodes after the request ends. The Go reader preserves its existing strict
UTF-8 and base64 validation. Reader tests also cover I/O failures, read ownership,
invalid limits and the largest positive int; `FuzzDecodeReader` checks chunk
boundaries and consumed bytes against the complete-body decoder.

Malformed-input tests live in `codec_test.go`. The Go decoder deliberately rejects
noncanonical base64 (missing padding, nonzero padding bits, whitespace or the URL
alphabet); Node's `Buffer.from(..., "base64")` accepts some of these. Valid output
from the reference encoder is canonical. This preparation does not claim identical
error recovery: the Go codec returns no partial packets when any record is invalid.

`BenchmarkDecode/dense-records` measures heap amplification: a 999,999-byte body
holds 500,000 empty MESSAGE packets. The caller's wire limit does not bound the
decoded heap to the same byte count. The accepted approach follows JS polling:
limit the HTTP body size, decode the complete batch into memory, and impose no
additional packet-count limit. Transport integration must bound HTTP reads before
buffering; reducing the decoded batch's memory use is a future optimization, not an
integration prerequisite. The TODO in `Decode` marks where to investigate
incremental consumption while preserving acceptance of payloads within the byte
limit. Use the dense-record benchmark to assess improvements. The current codec is
staged internally and is not connected to v1.
