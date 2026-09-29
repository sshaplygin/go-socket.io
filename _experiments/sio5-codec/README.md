# Socket.IO protocol 5 wire experiment

This standalone Go 1.22 module prepares the wire portion of roadmap stage 2.3.
It is excluded from the root module's `./...` checks and changes no live server,
client, transport, or public parser API. The G2 integration gate has **not** passed.
`Packet`, `Group`, and their signatures are provisional: reconcile them with the
stage 2.0 API fixture before moving code into the production parser.

The implementation was checked against the updated roadmap snapshot SHA-256
`29fb1a18030e7277ee1b43839d70b58d72e373bbb835ab1f93eef5b85ad56a28`.
This document describes the experiment; the repository roadmap owns the overall
stage sequence and runtime contracts.

## Implemented scope

- `Encode`/`Decode`: complete text envelopes for CONNECT, DISCONNECT, EVENT, ACK,
  CONNECT_ERROR, BINARY_EVENT, and BINARY_ACK, including namespaces, optional IDs,
  attachment counts, JSON validation, reserved event names, and payload shapes.
- `Packet.Data`: owned `json.RawMessage`, preserving JSON spelling and whitespace.
  Application decoding is deferred. Validation still temporarily parses JSON;
  this is not a zero-allocation or zero-copy parser.
- `EncodeGroup`/`DecodeGroup`: a complete envelope plus an exact ordered set of
  attachments, validated as one unit. No partial result is returned on failure.
- `Reconstruct`: an explicitly requested generic JSON tree (`map[string]any`,
  `[]any`, `json.Number`) with nested placeholders replaced by owned `[]byte`.
  Typed Go struct traversal, `socketio.Binary` discovery/deconstruction, descriptor
  argument expansion, and application argument arity are not implemented.

`Decode` and `DecodeGroup` copy input JSON; complete-group APIs also copy binary
bytes. Encoding returns independent buffers. A reconstructed tree shares nothing
with caller-owned buffers. Repeated references to one attachment share its single
owned copy, matching Node's behavior. Unreferenced attachments and repeated
placeholder indices are accepted as in Node; count and byte budgets still apply.
Caller mutation concurrent with a call requires caller synchronization.

IDs range from zero through `2^53-1`, inclusive, preserving JavaScript integer
precision; an absent ID differs from ID zero. This codec does **not** allocate IDs.
Monotonic allocation without reuse across namespace reconnects for the entire
Engine.IO session, late/duplicate ACK handling, and `ErrAckIDExhausted` belong to
future session runtime work. The four `Event`/`AckEvent` × incoming-ID cases, typed
error-first acknowledgements, at-most-once responses, and pending-ACK lifecycle
also belong to that runtime. The wire layer accepts general protocol ACK arrays.

## Limits and incomplete input

`Limits` is experiment-local. Each zero field selects its bounded default;
negative fields return `ErrLimit`.

| Field | Unit | Default |
| --- | --- | --- |
| `MaxBytes` | UTF-8 envelope bytes plus all raw attachment bytes for a group | 1 MiB |
| `MaxAttachments` | attachment count per group | 64 |
| `MaxDepth` | nested JSON arrays/objects | 64 |

For envelope-only calls, `MaxBytes` applies to that envelope. Group calls count the
whole envelope, including headers and placeholders, plus attachments. Budget checks
use subtraction to avoid addition overflow. Exact boundaries are accepted.
`ErrTooLarge`, `ErrTooManyAttachments`, `ErrAttachments`, and `ErrDepth` distinguish
byte limits, count limits, malformed/missing attachments, and excessive nesting.
JSON heap allocations can exceed wire size; no measured heap multiplier is claimed.

Every API receives a **complete** envelope/group. A missing attachment is an error,
not a pending state. There is no incremental assembler, transport reader, queued
message ownership transfer, attachment timeout, or interleaving state machine.
The future integration must enforce the roadmap's attachment assembly timeout,
bound incomplete groups, preserve atomic envelope/attachment ordering, and apply
transport limits before buffering input. Passing this experiment's tests does not
satisfy those integration requirements.

## Node reference and deliberate differences

The optional oracle pins official [`socket.io-parser@4.2.7`](https://www.npmjs.com/package/socket.io-parser/v/4.2.7)
and its transitive dependency integrity hashes in `reference/package-lock.json`.
It uses the installed package's actual `Encoder` and `Decoder`; protocol version
is asserted to equal 5. The reference implementation can be inspected in
`reference/node_modules/socket.io-parser/build/cjs/{index,binary}.js` after `npm ci`.

Valid fixtures cover auth, CONNECT responses/errors, namespaces, Unicode, numeric
event names, ACK success/error arrays, zero and maximum IDs, nested binary values,
empty buffers, and binary ACKs. Three independent Go-origin packets are decoded by
Node and compared with Node encoding; fourteen Node-origin groups are decoded,
reconstructed, and re-encoded by Go, then decoded again by Node. Additional cases
assert shared malformed input rejection, intentional stricter handling, and limits.

Deliberate differences from the pinned Node decoder:

- Missing EVENT/ACK/CONNECT_ERROR data is rejected even though `Decoder.add` accepts
  an absent payload. CONNECT may omit data; DISCONNECT must omit it.
- Explicit namespace headers require the terminating comma, and namespace values
  must start with `/` and contain no comma, NUL, CR, or LF. Empty namespace selects `/`.
- Attachment headers use unsigned decimal digits and a positive count. Numeric
  coercions such as `1.0` or `1e0` are rejected. Version 4.2.7 also rejects a zero
  count, unlike some older parser versions. Its default attachment limit is 10;
  our default follows the roadmap's 64, and the oracle configures Node to 64.
- IDs above the JavaScript safe integer range are rejected. Leading zeros remain
  accepted, with canonical decimal encoding on output.
- Placeholder indices must identify integer positions; fractional indices are
  rejected instead of producing Node's `undefined` attachment value.
- Wire input must be valid UTF-8. Go's `encoding/json` replaces unpaired UTF-16
  surrogate escapes when reconstructing a string; Node retains those lone code
  units. Raw JSON envelope round-tripping preserves the original escapes. This
  generic reconstruction edge case remains a compatibility improvement for future
  parser integration; ordinary Unicode fixtures are covered by the oracle.
- Byte/depth limits and exact complete-group attachment counts are explicit API
  checks. Node's streaming decoder waits for missing attachments instead.

## Verification

Run from this directory; root CI does not run this standalone module:

```sh
gofmt -l .
go test -race -count=1 -cover ./...
go vet ./...
GOTOOLCHAIN=go1.22.12 go test ./...
go test -run='^$' -fuzz=FuzzEnvelope -fuzztime=10s -parallel=2
go test -run='^$' -fuzz=FuzzGroup -fuzztime=10s -parallel=2
npm ci --ignore-scripts --prefix reference
npm test --prefix reference
```

On macOS hosts where Go 1.22's internal linker is incompatible with the installed
system toolchain, the compatibility check can use
`GOTOOLCHAIN=go1.22.12 go test -ldflags=-linkmode=external ./...`.
Node is optional: ordinary Go tests require only the standard library.
`cmd/oracle` is a bounded-in-time test bridge launched by the Node script, not a
production service or a streaming decoder.
