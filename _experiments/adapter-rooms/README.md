# In-memory room selection reference

This isolated preparation artifact captures the installed Node in-memory
`Adapter` from **socket.io-adapter 2.5.5**, using **socket.io-parser 4.2.7** for
packet encoding. It supplies membership and recipient fixtures for the future
stage 2.2 Go memory adapter. Integration prerequisites and acceptance gates remain
owned by [the roadmap](../../docs/ROADMAP.md); this corpus does not pass those
gates or establish the final Go adapter API.

`reference/package-lock.json` pins the complete Node dependency tree. The oracle
is `reference/node_modules/socket.io-adapter/dist/in-memory-adapter.js`, specifically
`addAll`, `del`, `delAll`, `sockets`, `socketRooms`, `broadcast`, `apply`, and
`computeExceptSids`. No copy of those methods implements the oracle here.

## Reproduce

From this directory, with Go 1.22+ and Node/npm installed:

```sh
cd reference
npm ci --ignore-scripts
npm test
cd ..
go test -race ./...
go vet ./...
golangci-lint run --allow-serial-runners ./...
GOTOOLCHAIN=go1.22.12 go test -ldflags=-linkmode=external ./...
```

The external linker is needed for the Go 1.22 toolchain on the validation host
(macOS 26); it is not a library requirement. Capture was validated with Node
26.4.0/npm 11.17.0. `npm test` executes the installed adapter again and compares
the entire canonical JSON byte-for-byte without modifying the fixture. After an
intentional, reviewed oracle or case change, regenerate explicitly:

```sh
cd reference
npm run capture
npm test
```

The only generated artifact is `testdata/rooms.json`. Go checks need no Node
installation or network access; the Go module has no third-party dependencies.

## Corpus and harness

The 22 named cases record each setup action with its resulting `rooms` and `sids`
maps and live namespace socket IDs, lifecycle events, `sockets`/`socketRooms`
queries, encoded broadcast write invocations and outgoing notifications. Cases
cover empty/all selection, room union and duplicate removal, unknown rooms and
IDs, exclusions, overlapping memberships, leave cleanup and idempotence, empty
memberships, absent live sockets, local/volatile/compression flags, and empty or
Unicode room names.

The fake namespace supplies a real parser encoder and a socket map. A harness
`connect` action registers a fake socket and explicitly calls `addAll` with its
own ID room plus supplied rooms, modeling normal Socket.IO connection setup.
Automatic ID-room membership is a **harness setup action**, not something
`Adapter.addAll` itself does. The fake socket records the real adapter's outgoing
notification and `client.writeToEngine` calls. No server, Redis connection, timer,
transport, or actual outbound queue is started.

Selected observations from this pinned oracle:

- `except` contains **room names**. Excluding a socket ID works through its ID
  room; another socket that joined that same room is also excluded.
- Union broadcasts invoke each selected live socket once. An empty target set
  selects all registered adapter SIDs that are present in the live socket map.
- Removing the final room with `del` retains an empty SID entry. `addAll` with an
  empty set also creates an empty SID entry. `delAll` removes the SID entry.
- Membership can reference an ID absent from the namespace socket map. It remains
  visible in snapshots/`socketRooms` but receives no broadcast invocation.
- `local` does not change local selection and is not forwarded as a write option.
  `volatile` and `compress`, including explicit `false`, are forwarded along with
  `preEncoded: true`. The fake client proves invocation and option forwarding;
  it does **not** prove delivery, volatile dropping, backpressure, or compression.

Map/set members and recipient lists are sorted for deterministic comparison.
Lifecycle traces retain observed Node order for diagnostic provenance; no Go
recipient or callback ordering guarantee is implied. Upstream optional
`wsPreEncodedFrame` is intentionally omitted from the recorded write options:
WebSocket frame precomputation is outside membership/selection scope. The encoded
Socket.IO packet itself is retained.

`internal/fixture` strictly decodes the known schema and validates map backlinks,
canonical sets, static query/recipient membership, null versus empty room results,
write flags, packet contents, notifications and lifecycle event arities. Tests
reject malformed JSON and deliberately corrupted relationships. This is trusted
fixture tooling, not a production parser or adapter: it does not replay mutations
or validate lifecycle event order independently. Regenerating with the actual
Node package is the authority for those traces.

## Reuse and remaining limits

A future Go conformance test can replay the setup operations through its finalized
adapter interface, compare membership/query sets, and collect enqueue attempts
for the expected recipients. Adapt ID-room setup at the Socket layer and compare
semantic sets, without making Go iterate in Node's insertion order. Shared room
selection is complementary to the sibling `adapter-wire` publication fixtures;
there is no copied Redis fixture engine, cluster broadcast, broadcast ACK, or
server-side emit scope here.

The Go implementation still must independently verify partial enqueue results,
bounded queues, concurrent join/leave/broadcast, and that no room lock is held
while a recipient blocks. Synchronous JS capture cannot prove any Go locking or
race property. Runtime lifecycle behavior and transport delivery remain outside
this corpus.

On the capture host, `npm audit --json` reported **2 affected packages**: one
moderate report on `socket.io-adapter`, inherited through `ws`, and one high report
on transitive **ws 8.17.1** (including
[GHSA-58qx-3vcg-4xpx](https://github.com/advisories/GHSA-58qx-3vcg-4xpx) and
[GHSA-96hv-2xvq-fx4p](https://github.com/advisories/GHSA-96hv-2xvq-fx4p)). The exact
reference pins are retained deliberately. This local capture tool creates no
network listener and accepts no untrusted packets; Node packages are not Go
runtime dependencies. This is not a claim of a vulnerability-clean dependency tree.
