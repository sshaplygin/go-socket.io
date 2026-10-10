# Node adapter wire fixtures

`publications.json` holds the publications of the non-sharded Node Redis adapter that
the `adapter/codec` tests decode and re-encode. `reference/` is the Node program that
captured them and that checks the codec's output with Node's own decoders. The format
the codec implements is described in its godoc; the stage plan is in
[docs/ROADMAP.md](../../../docs/ROADMAP.md) (2.2 and 4b). This file covers where the
fixtures come from and how to reproduce them.

## Reproduce

Go side, from the repository root (Go 1.22+; no Node needed):

```sh
go test -race -count=1 ./adapter/codec
```

Node side, from this directory:

```sh
npm ci --ignore-scripts --no-audit --no-fund --prefix reference
npm test --prefix reference
```

`npm test` first runs `capture.cjs`, which reproduces the captured bytes and channels
exactly. Then `verify-go.cjs` runs `TestRoundtripExport` (the codec decodes and
re-encodes every publication), decodes the Go-produced bytes with `notepack.io` or
`JSON.parse` and compares values, binary types included. Run `npm run capture
--prefix reference` only for an intentional fixture update and review the diff; the
ordinary test never rewrites fixtures.

## Provenance and scope

Pinned upstream packages (integrity hashes in `reference/package-lock.json`):

- `@socket.io/redis-adapter@8.3.0`
- `socket.io-adapter@2.5.5`
- `socket.io-parser@4.2.7`
- `notepack.io@3.0.1`

The capture program calls the installed adapter's broadcast and request methods. It
substitutes a deterministic `uid2` result, an in-memory namespace and socket, and Redis
pub/sub stubs. Stubbed subscriber counts and replies finish outgoing requests; they do
not test Redis itself. Subscription setup, channel selection, serialization and inbound
request handling use the upstream implementation. A synthetic incoming request produces
the response fixtures through that handler; they are not observations of a remote
process.

The 22 scenarios cover the root and custom namespaces and prefixes, one and several
rooms, exclusions, flags, nested and empty binary, local suppression, broadcast with
ack, join, leave and disconnect requests, server-side emit with and without ack,
rooms and sockets queries, common and specific response channels and binary broadcast
acknowledgements. This is a selected corpus, not every legacy request variant or Redis
behavior.

Four publications are reference-only observations of what the pinned adapter does, not
messages `adapter/codec` implements (the cluster broadcast with acknowledgements is
excluded from stage 4b, and server-side emit is publication-only): `broadcast-ack-request`,
`broadcast-ack-responses` (two publications) and `server-emit-response`. The codec
rejects them with `ErrUnsupported`, which the tests pin, and the Node check re-encodes
them with a generic encoder. The join, leave and disconnect requests prepare the stage 5
capability and do not change the local room operations of stage 2.2.

Two wire distinctions are deliberate:

- Normal broadcasts and broadcast acks use MessagePack; the administrative requests and
  responses and server-side emit use JSON in this pinned adapter. Binary data in a JSON
  server-side emit becomes the Node `Buffer.toJSON()` object; the corpus records this
  rather than claiming binary preservation there.
- `$binary` in the `value` fields is a test-only base64 marker. Actual MessagePack
  publications carry bin values. For the supported publications the codec's output is
  byte-identical to Node's (the Go test asserts it); the Node check still compares
  decoded values, because a different encoder may legally order keys or choose integer
  widths differently.

Reference: upstream [adapter source at 8.3.0](https://github.com/socketio/socket.io-redis-adapter/blob/8.3.0/lib/index.ts).
