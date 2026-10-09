# Node adapter wire fixtures

This isolated experiment captures the non-sharded Redis adapter's actual
publications for later `adapter/codec` work. It does not implement a Go adapter,
room membership, broker transport, request aggregation or cluster conformance.
The live v1 runtime and root module dependencies are unchanged. Integration follows
the stage 1/1b and API/Packet contract gates in
[issue #2](https://github.com/sshaplygin/go-socket.io/issues/2).

## Reproduce

From this directory (Go 1.22+; Node is optional for Go-only fixture tests):

```sh
go test -race -count=1 ./...
go vet ./...
npm ci --ignore-scripts --no-audit --no-fund --prefix reference
npm test --prefix reference
```

The Go tests decode checked-in bytes with `vmihailenco/msgpack/v5@v5.4.1` and
`encoding/json`. The Node test first reproduces the captured bytes and channels
exactly, then decodes Go-reencoded versions and compares values, including binary
types. Run `npm run capture --prefix reference` only for an intentional fixture
update, and review the resulting diff. The ordinary test never rewrites fixtures.
Root `go test ./...` excludes this standalone module; run these commands explicitly.

## Provenance and scope

Pinned upstream packages (integrity hashes in `reference/package-lock.json`):

- `@socket.io/redis-adapter@8.3.0`
- `socket.io-adapter@2.5.5`
- `socket.io-parser@4.2.7`
- `notepack.io@3.0.1`

The capture program calls the installed adapter's broadcast and request methods.
It substitutes a deterministic `uid2` result, an in-memory namespace/socket, and
Redis pub/sub stubs. Stubbed subscriber counts/replies finish outgoing requests;
they do not test Redis itself. Subscription setup, channel selection, serialization
and inbound request handling use the actual upstream implementation. A synthetic
incoming request produces the response fixtures via that handler; it is not
represented as an observed publication from a remote process.

The 22 scenarios include root/custom namespaces and prefixes, one/multiple rooms,
exclusions, flags, nested/empty binary, local suppression, broadcast with ack,
join/leave/disconnect requests, server-side emit with/without ack, rooms/sockets
queries, common/specific response channels and binary broadcast acknowledgements.
This is a selected corpus, not all legacy request variants or Redis behavior.

The updated roadmap pins this same Redis adapter version. Its stage 4b scope
excludes cluster broadcast-with-ack and specifies publication-only server-side
emit. The `broadcast-ack-request`, `broadcast-ack-responses`,
`server-emit-ack-request` and `server-emit-response` cases are reference-only
observations, not requirements or implemented features of the planned adapter.
Bulk join/leave/disconnect request fixtures prepare the later stage 5 capability;
they do not change stage 2.2's local room-operation contract. Keep these categories
separate when selecting a future conformance corpus. Canonical Node bytes and
semantic cross-decoding match the revised stage 4b fixture policy.

Scope was checked against the working roadmap snapshot with SHA-256
`29fb1a18030e7277ee1b43839d70b58d72e373bbb835ab1f93eef5b85ad56a28`.
This experiment does not freeze the production API or satisfy G2/M5.

Two wire distinctions are deliberate:

- Normal broadcasts and broadcast-ack messages use MessagePack; many administrative
  requests/responses and server-side emit use JSON in this pinned adapter. Binary
  data in a JSON server-side emit becomes the Node `Buffer.toJSON()` object; the
  corpus records this behavior rather than claiming binary preservation there.
- `$binary` in fixture `value` fields is a test-only base64 marker. Actual
  MessagePack publications carry bin values. Object key order and legal integer
  widths can differ in Go-produced bytes; interoperability compares decoded values,
  while regenerated Node fixtures must still match every captured byte.

The Go `internal/fixture` package and `cmd/roundtrip` are test tooling for this
trusted corpus, not a hardened runtime decoder. Future adapter implementation
still needs resource limits, routing/aggregation, timeout and reconnect behavior,
ownership, and actual Go/Node broker integration tests.

Reference: upstream [adapter source at 8.3.0](https://github.com/socketio/socket.io-redis-adapter/blob/8.3.0/lib/index.ts).
