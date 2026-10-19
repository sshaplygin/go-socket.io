# V1 interoperability tests

The wire references are Socket.IO 2.5.0, Engine.IO 3.6.2,
`socket.io-parser` 3.4.5, `engine.io-parser` 2.2.1,
`socket.io-adapter` 1.1.2 and `socket.io-redis` 5.4.0.
The required JavaScript clients are `socket.io-client` 1.7.4 and 2.5.0.
Go client tests also connect to the reference Node server.

The table identifies test owners; it is not a claim of complete Node API parity.
Supported wire behavior and differences are specified in
[PROTOCOL.md](PROTOCOL.md), and Go API usage in [V1.md](V1.md).

| Contract | Behavioral evidence |
| --- | --- |
| Namespace registration, middleware, room selection, broadcast flags and listeners | Root package integration, connection and namespace tests |
| Connection admission, shutdown, queue overflow, cancellation and namespace generations | Server and client lifecycle tests, exercised under `-race` |
| Event/ACK arguments, variadic callbacks, reserved names and binary frame alignment | Packet-handler and integration tests in the root and `client` packages |
| Typed payloads and error-first ACKs | Root `event_integration_test.go` and `connection_typed_events_test.go`, `client/connection_typed_test.go`, typed consumer fixtures and Node typed-ACK tests |
| Socket.IO packet encoding, nested binary, exact integers, malformed input and cycles | `parser` unit tests, fuzz targets and Node conformance fixtures |
| Engine.IO handshake, heartbeat, polling/websocket upgrade and rejection responses | `engineio` / private session tests and raw conformance tests |
| Transport bounds, compression, polling batches and I/O errors | Polling, websocket and engine client tests |
| Local room unions, exclusions and lock release during delivery | `adapter` tests |
| Redis publication, queries, remote operations, timeouts, subscription readiness and reconnection | `adapter/redis` tests; mixed Go/Node cluster tests with real Redis |
| Go client reconnect, pre-connect buffering, namespace lifecycle and transport selection | `client` integration tests and private manager tests |
| Public consumer contracts, independent client/server roles and package privacy | `internal/parity` compilation fixtures and `.github/architecture` tests |
| Runnable chat, ACK, binary, namespaces and middleware examples | `make examples` and `make examples-node` |

`go test ./...` runs local tests without Node or Redis services. `make conformance`
runs the tagged protocol and interoperability suite; `make examples-node` runs the
JavaScript example clients, including two-process Redis examples. Missing external
dependencies fail those explicit targets. Setup is in
[CLAUDE.md](../CLAUDE.md#interoperability) and
[_examples/README.md](../_examples/README.md).
