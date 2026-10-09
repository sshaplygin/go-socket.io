# v2 API skeleton

The frozen declarations of roadmap stage 2.0: the method-signature inventory and the
package graph that parallel work (2.1 to 2.4) builds against. The skeleton is
declarations only. Every operation that needs the runtime returns
`socketio.ErrNotImplemented`; nothing here works yet. Behaviour is specified in
[ROADMAP.md](ROADMAP.md) (sections 2.0 to 2.4) and, once it exists, in the godoc of each
declaration. This file lists signatures and does not restate behaviour. A signature
change updates this file, the fixtures and the owning roadmap section in one PR.

`ctx` is `context.Context`, `Args` is `parser.Arguments`. Both lists are checked:
`TestInventoryListsEveryExportedSignature` fails when an exported identifier of the root
package has no entry below, and `make graph` checks the package graph.

## Method-signature inventory

### Root package `socketio`

| File | Declaration | Signature |
| --- | --- | --- |
| `event.go` | `NewEvent` | `NewEvent[T any](name string) Event[T]` |
| `event.go` | `Event.Name` | `(Event[T]) Name() string` |
| `event.go` | `Event.Handle` | `(Event[T]) Handle(*Namespace, func(ctx, *Socket, T) error) error` |
| `event.go` | `Event.HandleClient` | `(Event[T]) HandleClient(ClientRegistration, func(ctx, Endpoint, T) error) error` |
| `event.go` | `Event.Emit` | `(Event[T]) Emit(ctx, Endpoint, T) error` |
| `event.go` | `Event.EmitTo` | `(Event[T]) EmitTo(ctx, BroadcastOperator, T) (BroadcastResult, error)` |
| `event.go` | `NewAckEvent` | `NewAckEvent[T, R any](name string) AckEvent[T, R]` |
| `event.go` | `AckEvent.Name` | `(AckEvent[T, R]) Name() string` |
| `event.go` | `AckEvent.Handle` | `(AckEvent[T, R]) Handle(*Namespace, func(ctx, *Socket, T) (R, error)) error` |
| `event.go` | `AckEvent.HandleClient` | `(AckEvent[T, R]) HandleClient(ClientRegistration, func(ctx, Endpoint, T) (R, error)) error` |
| `event.go` | `AckEvent.EmitWithAck` | `(AckEvent[T, R]) EmitWithAck(ctx, Endpoint, T) (R, error)` |
| `event.go` | `Event`, `AckEvent` | generic descriptors; the payload, handler and ack types stay on the descriptor |
| `event.go` | `Args2` | `Args2[A, B any]{First A; Second B}`: exactly two positional arguments |
| `event.go` | `Binary` | `type Binary []byte` |
| `event.go` | `Binary.SocketIOBinary` | `(Binary) SocketIOBinary() []byte`; implements `parser.BinaryValue` |
| `packet_handlers.go` | `Endpoint` | `SendPacket(ctx, parser.Packet) error`; `RequestAck(ctx, parser.Packet) (Args, error)` |
| `packet_handlers.go` | `ClientRegistration` | embeds `Endpoint`; `RegisterEvent(string, ClientRawHandler) error` |
| `packet_handlers.go` | `RawHandler` | `func(ctx, *Socket, RawEvent) error` |
| `packet_handlers.go` | `ClientRawHandler` | `func(ctx, Endpoint, RawEvent) error` |
| `packet_handlers.go` | `RawAck` | `Respond(ctx, Args) error` |
| `packet_handlers.go` | `RawEvent` | `{Name string; Args Args; Ack RawAck}`; `Ack` is nil when no ack was requested |
| `server.go` | `NewServer` | `NewServer(Options) (*Server, error)`; creates no namespace, `/` included |
| `server.go` | `Server.Namespace` | `(*Server) Namespace(ctx, name string) (*Namespace, error)`; the creating call (ROADMAP 2.2 *Readiness*) |
| `server.go` | `Server.Shutdown` | `(*Server) Shutdown(ctx) error` |
| `server.go` | `Server.Close` | `(*Server) Close() error` |
| `server.go` | `Server` | server type |
| `namespace.go` | `Namespace.Name` | `(*Namespace) Name() string` |
| `namespace.go` | `Namespace.Use` | `(*Namespace) Use(Middleware) error` |
| `namespace.go` | `Namespace.OnRaw` | `(*Namespace) OnRaw(RawHandler) error` |
| `namespace.go` | `Namespace.To` | `(*Namespace) To(...Room) BroadcastOperator` |
| `namespace.go` | `Namespace.Hooks` | `(*Namespace) Hooks() *Hooks`; nil-safe, for adapters in other modules |
| `namespace.go` | `Namespace.Logger` | `(*Namespace) Logger() *slog.Logger`; nil-safe |
| `namespace.go` | `Namespace` | namespace type |
| `namespace.go` | `Middleware` | `func(ctx, *Socket, json.RawMessage) error` |
| `namespace.go` | `Auth` | `Auth[T any](func(ctx, *Socket, T) error) Middleware` |
| `namespace.go` | `BroadcastResult` | `{LocalRecipients int; Published bool}` |
| `namespace.go` | `BroadcastOperator` | immutable selection; `Except(...Room) BroadcastOperator`, `Local() BroadcastOperator` |
| `namespace.go` | `BroadcastOperator.Except` | `(BroadcastOperator) Except(...Room) BroadcastOperator` |
| `namespace.go` | `BroadcastOperator.Local` | `(BroadcastOperator) Local() BroadcastOperator` |
| `socket.go` | `Socket` | implements `Endpoint` |
| `socket.go` | `Socket.ID` | `(*Socket) ID() SocketID` |
| `socket.go` | `Socket.SendPacket` | `(*Socket) SendPacket(ctx, parser.Packet) error` |
| `socket.go` | `Socket.RequestAck` | `(*Socket) RequestAck(ctx, parser.Packet) (Args, error)` |
| `socket.go` | `Socket.Join` | `(*Socket) Join(...Room) error` |
| `socket.go` | `Socket.Leave` | `(*Socket) Leave(...Room) error` |
| `socket.go` | `Room` | `type Room string` |
| `socket.go` | `SocketID` | `type SocketID = Room`, so `nsp.To("room").Except(s.ID())` compiles |
| `adapter.go` | `Adapter` | `AddAll(SocketID, []Room)`; `Del(SocketID, Room)`; `DelAll(SocketID)`; `Broadcast(ctx, parser.Packet, BroadcastOptions) (BroadcastResult, error)`; `Sockets(ctx, []Room) ([]SocketID, error)`; `SocketRooms(SocketID) []Room`; `FetchSockets(ctx, BroadcastOptions) ([]RemoteSocket, error)`; `ServerSideEmit(ctx, string, ...any) error`; `Close() error` |
| `adapter.go` | `AdapterFactory` | `func(ctx, nsp *Namespace) (Adapter, error)` |
| `adapter.go` | `BroadcastOptions` | `{Rooms, Except []Room; Flags BroadcastFlags}` |
| `adapter.go` | `BroadcastFlags` | `{Local bool}` |
| `adapter.go` | `RemoteSocket` | `{ID SocketID; Rooms []Room; Handshake, Data json.RawMessage}` |
| `options.go` | `Options` | `{Logger *slog.Logger; Engine engineio.Options; Hooks *Hooks; Adapter AdapterFactory; AckTimeout time.Duration; OutboundQueueGroups, OutboundQueueBytes, HandlerQueueEvents, HandlerQueueBytes, MaxPendingAcks, MaxAttachments, MaxEventBytes, MaxConcurrentConnects int; AttachmentTimeout, ConnectTimeout time.Duration}`; defaults and units in the godoc |
| `options.go` | `Options.Normalize` | `(Options) Normalize() (Options, error)` |
| `options.go` | `Hooks` | 16 fields, signatures below |
| `options.go` | `SocketInfo` | `{SID, SocketID, Namespace string}` |
| `options.go` | `EventInfo` | `{Socket SocketInfo; Event string; AckID uint64; NeedAck, HandlerFound bool}` |
| `options.go` | `EventResult` | `{Result string; Err error; AckQueued bool; Duration time.Duration}` |
| `options.go` | `EmitInfo` | `{Socket SocketInfo; Event string; AckID uint64}` |
| `options.go` | `BroadcastInfo` | `{Namespace, Event string; Rooms, Except []Room; Local bool}` |
| `options.go` | `AdapterMessage` | `{Namespace, Kind string; Bytes int}` |
| `options.go` | result values | `EventResultOK`, `EventResultError`, `EventResultNoHandler`, `EventResultDecodeError`, `EventResultClosed`; `ConnectResultOK`, `ConnectResultError`, `ConnectResultUnknownNamespace`; `AckResultOK`, `AckResultTimeout`, `AckResultClosed`, `AckResultCanceled`, `AckResultError`; `AdapterMessageBroadcast`, `AdapterMessageRequest`, `AdapterMessageResponse` |
| `errors.go` | `ErrNotImplemented` | returned by every skeleton operation that needs the runtime |
| `errors.go` | runtime errors | `ErrNamespaceClosed`, `ErrAckTimeout`, `ErrWriteBufferFull`, `ErrSocketClosed`, `ErrTooManyPendingAcks`, `ErrMessageTooLarge`, `ErrTooManyAttachments`, `ErrAckIDExhausted`; match with `errors.Is` |

Socket.IO hook fields (`socketio.Hooks`):

| Field | Function signature |
| --- | --- |
| `ConnectStart` | `func(ctx, SocketInfo) ctx` |
| `ConnectEnd` | `func(ctx, SocketInfo, error, time.Duration)` |
| `Disconnect` | `func(ctx, SocketInfo, string, time.Duration)` |
| `EventStart` | `func(ctx, EventInfo) ctx` |
| `EventEnd` | `func(ctx, EventInfo, EventResult)` |
| `Emit`, `MessageQueued` | `func(ctx, EmitInfo)` |
| `EmitStart` | `func(ctx, EmitInfo) ctx` |
| `EmitEnd` | `func(ctx, EmitInfo, error, time.Duration)` |
| `BroadcastStart` | `func(ctx, BroadcastInfo) ctx` |
| `BroadcastEnd` | `func(ctx, BroadcastInfo, BroadcastResult, error)` |
| `WriteBufferFull` | `func(ctx, SocketInfo)` |
| `AdapterPublishStart`, `AdapterReceiveStart` | `func(ctx, AdapterMessage) ctx` |
| `AdapterPublishEnd`, `AdapterReceiveEnd` | `func(ctx, AdapterMessage, error, time.Duration)` |

### Packages `engineio` and `parser`

Additions only; nothing existing changed.

| File | Declaration | Signature |
| --- | --- | --- |
| `engineio/hooks.go` | `Hooks` | 11 fields, signatures below |
| `engineio/hooks.go` | `SessionInfo`, `PacketInfo`, `HandshakeResult` | `{SID, Transport, RemoteAddr string}`; `{Type packet.Type; Frame frame.Type; Bytes int; Preview []byte}`; `{Result string; Err error; Duration time.Duration}` |
| `engineio/hooks.go` | `CloseReason` | `type CloseReason string` with `CloseTransportClose`, `CloseTransportError`, `ClosePingTimeout`, `CloseForced`, `CloseServerShuttingDown`, `CloseParseError`; handshake results `HandshakeResultOK`, `HandshakeResultBadTransport`, `HandshakeResultChecker`, `HandshakeResultAccept`, `HandshakeResultNoHijacker`, `HandshakeResultInit`, `HandshakeResultTimeout`, `HandshakeResultClosed` |
| `engineio/hooks.go` | `PayloadRedactor` | `Enabled(ctx) bool`; `Redact(ctx, SessionInfo, PacketInfo, []byte, int) []byte` |
| `engineio/options.go` | `Options` fields | `Hooks *Hooks`; `PayloadPreviewBytes int` (0 to 256); `PayloadRedactor PayloadRedactor` |
| `engineio/options.go` | `Options.Normalize` | `(Options) Normalize() (Options, error)`; rejects a preview limit outside 0..256 |
| `parser/value.go` | `Arguments` | `{Values []json.RawMessage; Attachments [][]byte}` |
| `parser/value.go` | `Packet` | `{Type Type; Namespace string; ID *uint64; Data json.RawMessage; Attachments [][]byte}` |
| `parser/value.go` | `BinaryValue` | `SocketIOBinary() []byte` |
| `parser/value.go` | `ArgumentCodec` | `ArgumentCodec[T any]{Encode func(T) (Arguments, error); Decode func(Arguments) (T, error)}` |

Engine.IO hook fields (`engineio.Hooks`):

| Field | Function signature |
| --- | --- |
| `HandshakeStart` | `func(ctx, *http.Request) ctx` |
| `HandshakeEnd` | `func(ctx, SessionInfo, HandshakeResult)` |
| `RequestRejected` | `func(ctx, *http.Request, string, error)` |
| `SessionOpen` | `func(ctx, SessionInfo) ctx` |
| `SessionClose` | `func(ctx, SessionInfo, CloseReason, error, time.Duration)` |
| `UpgradeStart` | `func(ctx, SessionInfo, string, string) ctx` |
| `UpgradeEnd` | `func(ctx, SessionInfo, string, string, error)` |
| `PacketRead`, `PacketWrite` | `func(ctx, SessionInfo, PacketInfo)` |
| `PingSent` | `func(ctx, SessionInfo)` |
| `PongReceived` | `func(ctx, SessionInfo, time.Duration)` |

### Not declared in 2.0

Signatures that need the runtime design and are therefore left to their stage rather
than declared as placeholders: the HTTP handler of `Server`, the lifecycle context of
`Socket` and `Namespace`, socket disconnect, connection callbacks, `ChainHooks`,
`LoggingHooks`, the in-memory adapter and the `client/` package (the Go client).
`internal/fixtures/clientstub` implements `ClientRegistration` for the fixtures only.
The upgrade results, request-rejection reasons, disconnect reasons and adapter results
of the hook contracts are not enumerated by the roadmap and have no constants.

## Package graph

```text
socketio (.)  ──> engineio ──> engineio/{frame,packet,payload,session,transport/...,internal}
     │                 └─────> logger
     └──────────> parser ──> engineio/frame, logger
internal/fixtures/{positive,externaladapter,clientstub} ──> socketio, parser
internal/fixtures/positive ──> engineio, engineio/{frame,packet}
logger ──> standard library only
```

Rules, enforced by `TestPackageGraph` (also a depth-first cycle search):

- the root imports only `engineio` and `parser`;
- only the root's fixtures under `internal/fixtures` import the root; `engineio/...`,
  `parser` and `logger` never do, so the future `client/` and external adapters import
  the root and the root never imports them;
- `engineio/...` never imports `parser`: the Socket.IO layer supplies payload
  redaction;
- `parser` imports only `engineio/frame` and `logger`; `logger` imports nothing of this
  module.

Command: `make graph` (runs `go test -count=1 -run '^TestPackageGraph$' .`; set
`SOCKETIO_PRINT_GRAPH=1` and `-v` to print every edge).

## Compile fixtures

`internal/fixtures/positive` (server and client registration, emit, ack, room
broadcast, `Args2`, binary data, the creating call with `ctx` and an `AdapterFactory`
taking `ctx`, both hook structs, option composition) must compile. Each directory of
`testdata/negative` must fail with every fragment of its `diagnostic.txt` inside its
`invalid.go`: wrong handler argument or return, ack request, result or return type,
emitted or broadcast payload, `Args2` order, auth handler, client handler, adapter
factory without `ctx`, adapter method set, creating call without `ctx` or error, hook
return and layer, Engine.IO packet identity and redactor result. `TestCompileContracts`
runs both and is part of `go test ./...`, so the `min-go` job runs it on Go 1.22 with
`GOTOOLCHAIN=local`.
