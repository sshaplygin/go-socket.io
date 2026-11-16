# V1 interoperability tests

The wire references are Socket.IO 2.5.0, Engine.IO 3.6.2,
`socket.io-parser` 3.4.5, `engine.io-parser` 2.2.1,
`socket.io-adapter` 1.1.2 and `socket.io-redis` 5.4.0.
The required JavaScript clients are `socket.io-client` 1.7.4 and 2.5.0.
Go client tests also connect to the reference Node server.

The table identifies test owners; it is not a claim of complete Node API parity.
Supported wire behavior and differences are specified in
[PROTOCOL.md](PROTOCOL.md), Go API usage in [V1.md](V1.md), and the JavaScript API
without a counterpart in its
[list of differences](V1.md#differences-from-the-socketio-2x-javascript-api).

| Contract | Behavioral evidence |
| --- | --- |
| Namespace registration, middleware, room selection, broadcast flags and listeners | Root package integration, connection and namespace tests |
| Connection admission, shutdown, queue overflow, cancellation and namespace generations | Server and client lifecycle tests, exercised under `-race` |
| Event/ACK arguments, variadic callbacks and binary frame alignment | Packet-handler and integration tests in the root and `client` packages |
| Reserved and local event names stay off the wire | Root `TestReservedEmitStaysLocal`, `TestReservedEmitCallsLocalListenersOnce` and `TestPeerCannotDispatchLifecycle`; `client` `TestClientLocalEvents*` and `TestClientTypedSendsRejectLocalEvents`; `internal/conformance` `TestGoClientKeepsLocalEventsOffTheWire` against the Node server |
| Typed payloads and error-first ACKs | Root `event_integration_test.go` and `connection_typed_events_test.go`, `client/connection_typed_test.go`, typed consumer fixtures and Node typed-ACK tests |
| Error results of untyped handlers and recovered callback panics: the peer's reply, one `OnError` call or WARN record with the stack, and the connection state | Root `handler_errors_test.go` and `internal/conformance` `TestNodeHandlerErrorsAndPanics` with both pinned JavaScript clients over polling and websocket |
| Socket.IO packet encoding, nested binary, exact integers, malformed input and cycles | `parser` unit tests, fuzz targets and Node conformance fixtures |
| Engine.IO handshake, polling/websocket upgrade and rejection responses | `engineio` / private session tests and raw conformance tests |
| Engine.IO heartbeat and missing-ping timeout, with `CLOSE` for a poll pending at the timeout | `engineio/internal/session` `TestPingTimeoutIdleClient` and `TestHeartbeatWindowIsIntervalPlusTimeout`, `engineio` `TestPingPostReleasesBeforePongPoll`, `TestHeartbeatDefaults`, `TestPendingPollAtPingTimeoutReceivesClose` and `TestPendingPollAtRequestDeadlineReceivesClose`, `engineio/transport/polling` `TestPollFailureAnswer` and `TestPollStartedResponseOutranksCloseAnswer`, `client` `TestPollingClientAtServerPingTimeoutReportsTransportClose`, and `internal/conformance` `TestJSHeartbeat` (`transport close` after the server's ping timeout) with both pinned JavaScript clients over polling and websocket |
| Transport bounds, compression, polling batches and I/O errors | Polling, websocket and engine client tests |
| Message limit, polling batches and 10 binary attachments with real clients | `internal/conformance` `TestDifferential` scenarios `large-payload`, `burst`, `client-burst` and `attachments`, and over websocket `over-limit` and `too-many-attachments`, compared with the Node server for both pinned JavaScript clients; over polling `TestPollingSessionEndingScenarios` checks only that both servers end the session, and root `TestServerEndedPollingSessionAnswersPendingPoll` pins this server's HTTP 500 for the pending poll |
| Websocket text messages `b<type><base64>` read as binary packets with or without `b64`, written only with `b64` | `engineio` `TestWebsocketBase64Frames`, `TestWebsocketBase64Malformed` and `TestWebsocketBase64MessageLimit`; `engineio/transport/websocket` `TestWebsocketBase64Packets` and `TestWebsocketBase64PacketsMalformed` for the server and Go client roles, and `FuzzPacketReader`; root `TestBinaryEventInBase64WebsocketMessage` (the frames of socket.io-client 1.3.7) and `TestBinaryEventInvalidBase64EndsSession` |
| The end of a session reaches `OnDisconnect` only, never `OnError` | Root `TestSessionEndSkipsOnError` (client close over each transport, abrupt drops, ping timeout), `TestMessageLimitEndsSession` and `TestWriteFailureEndsSession` (also in `client`), `client` `TestServerSessionEndSkipsOnError` (`Server.Close`, `EngineConn().Close()`) and `TestClientMessageLimitOption`, and `internal/conformance` `TestJSSocketClose` with both pinned JavaScript clients over polling, websocket and upgrade |
| With the 100 ms ping interval and 300 ms ping timeout of the test, a server handler that runs past the heartbeat window ends its client's Engine.IO connection and loses its ACK; while a Go client callback runs the client reads no `PONG`; a blocked handler or callback of one namespace stalls the other namespaces of that client; a handler that acknowledges from a goroutine keeps the connection; an `AckFunc` called after its connection ended writes nothing | Root `TestBlockingServerHandlerEndsItsConnection`, `TestBlockingClientHandlerStopsHeartbeat`, `TestBlockingHandlerStallsOtherNamespaces`, `TestBlockingClientHandlerStallsOtherNamespaces` and `TestDeferredAckKeepsHeartbeat` over polling and websocket, `TestAckFuncAfterDisconnectSendsNothing` |
| Arguments that fail to decode for an untyped handler or an `Emit` callback with a `*json.UnmarshalTypeError` keep the connection; with another error, such as a `time.Time` given `"bad"` or an attachment sent as a text frame, and for a packet that is not valid, the error is reported while connected and the connection then closes; a typed handler given `"bad"` for a `time.Time` keeps the connection; an `Emit` callback is not called when its connection ends | Root `TestArgumentTypeMismatchKeepsConnection`, `TestUndecodablePacketReportedBeforeClose`, `TestTypedHandlerDecodeErrorKeepsConnection`, `TestAckCallbackArgumentDecodeErrors`, `TestClientArgumentDecodeErrors`, `TestClientAckCallbackArgumentDecodeErrors` and `TestEmitCallbackDroppedWhenConnectionEnds` |
| Sends, room operations, broadcasts and closes called from application goroutines | Root `TestConcurrentUseFromApplicationGoroutines`, under `-race` |
| Websocket close code 1000 and `transport close` on a server-initiated close | `engineio/transport/websocket`, `engineio` and `engineio/client` close-frame tests, `internal/conformance` `TestJSServerCloseFrame` with both pinned JavaScript clients and `TestGoClientReconnect` against the Node server |
| Go client websocket close: code 1000 on `Close` and after its own ping timeout, none after a transport error, a failed handshake or for an upgrade probe it did not switch to | `engineio/transport/websocket` `TestDialClose`, `engineio/client` `TestClientWebsocketCloseFrame`, `engineio` `TestGoClientCloseIsTransportClose`, `client` `TestClientCloseSendsWebsocketCloseFrame` |
| `transport close` for a client websocket close frame with code 1000, 1001 or none; `transport error` for other codes and a dropped connection | `engineio` `TestPeerCloseFrameReason`, `TestPeerCloseFrameReasonWhileWriting` and `engineio/internal/session` `TestFrameWriterObservesPeerClose`, root `TestOnDisconnectPeerCloseFrame` and `TestOnDisconnectPeerCloseFrameWhileEmitting`, `internal/conformance` `TestJSEngineCloseFrame` with both pinned JavaScript clients |
| Broadcasts from the connection handler reach the connecting socket after its CONNECT; a rejected socket gets neither them nor `OnDisconnect` | Root `connect_broadcast_test.go` over raw polling, the Go client over websocket and a two-node Redis cluster, and `internal/conformance` `TestDifferential` scenario `connect-broadcast` with both pinned JavaScript clients |
| Local room unions, exclusions and lock release during delivery | `adapter` tests |
| Redis publication, queries, remote operations, timeouts, subscription readiness and reconnection | `adapter/redis` tests, including `TestRedisGoPacketWithNodeOracle`, which `make conformance` runs against the reference `notepack.io`; mixed Go/Node cluster tests with real Redis |
| Go client reconnect, pre-connect buffering, namespace lifecycle and transport selection | `client` integration tests and private manager tests |
| Public consumer contracts, independent client/server roles and package privacy | `internal/parity` compilation fixtures and `.github/architecture` tests |
| Runnable chat, ACK, binary, namespaces and middleware examples | `make examples` and `make examples-node` |
| User-guide behavior: lifecycle-name broadcasts, rooms per namespace, event middleware arguments, a reason given before closing a root connection, a poll answered after `http.Server.WriteTimeout` over HTTP/1.x, an aborted poll, the client bundle route and sessions local to one instance | Root `guide_behavior_test.go` |

`go test ./...` runs local tests without Node or Redis services. `make conformance`
runs the tagged protocol and interoperability suite; `make examples-node` runs the
JavaScript example clients, including two-process Redis examples. Missing external
dependencies fail those explicit targets. Setup is in
[CLAUDE.md](../CLAUDE.md#interoperability) and
[_examples/README.md](../_examples/README.md).
