# v2 generic API compile proof

This standalone Go 1.22 module prepares part of roadmap stage 2.0 while stages 1
and 1b proceed. It changes no production API, root dependency or CI workflow. Its
module path is experimental; it is not the future `/v2` release module.

**This artifact does not pass G2 or implement Socket.IO runtime.** It establishes
that the proposed descriptor signatures preserve payload, handler and ack types
across a separate client package. It also checks the roadmap Adapter and both
observer hook structs, an external-adapter import direction, and a proposed
observer/adapter option composition. Constructors of descriptors, immutable room
selectors, metadata accessors, the binary marker and option normalization work as
pure helpers. Every operation that would register, authenticate, connect, send,
acknowledge or close returns `ErrNotImplemented`. `NewServer` and `client.Dial`
return a nil object and that error. The positive fixtures are compiled, never run.
The external adapter mock is also compile-only: error-returning operations fail
with the sentinel; its void membership operations and slice-only getter panic
with that sentinel rather than simulate a successful adapter. Namespace observer
accessors return nil as explicitly documented prototype metadata.

## Verification

Run inside this directory:

```sh
go test -race -count=1 ./...
go vet ./...
golangci-lint run --allow-serial-runners ./...
gofmt -l .
GOTOOLCHAIN=go1.22.12 go test -count=1 ./...
GOTOOLCHAIN=go1.22.12 go vet ./...
go list -deps ./...
```

On recent macOS, the Go 1.22 test executable may need
`go test -ldflags=-linkmode=external -count=1 ./...` to avoid its old internal Mach-O
linker incompatibility. This does not change the source language or compiler.
There are no external dependencies. Root `go test ./...` skips `_experiments`, so
the module commands above must run explicitly; integration must add a CI job.

`TestCompileContracts` invokes the active Go toolchain. It first compiles the
server/client positive fixture, then checks 17 deliberately invalid programs:
handler argument/return, ack return, emitted payload, ack request/result,
client handler/ack return, broadcast payload, `Args2` argument order and auth
handler type, adapter broadcast result and factory, hook context return and layer,
Engine.IO packet identity, and redactor result. Each must fail in `invalid.go` with all recorded diagnostic
fragments. An import/toolchain failure alone cannot pass a negative fixture.
The compiler subprocesses have a one-minute deadline. Other tests check all
runtime stubs report the sentinel and that zero selects bounded defaults.

## Proposed signatures

This inventory describes the implemented preparation slice. `ctx` means
`context.Context`; `Args` means `parser.Arguments`. Full declarations and generic
parameter names are in [event.go](event.go), [model.go](model.go),
[parser/types.go](parser/types.go), [client/client.go](client/client.go) and
[options.go](options.go), [adapter.go](adapter.go), [hooks.go](hooks.go) and
[engineio/hooks.go](engineio/hooks.go).

| Owner | Signature |
| --- | --- |
| Root | `NewEvent[T](name string) Event[T]` |
| `Event[T]` | `Name() string`; `Handle(*Namespace, func(ctx, *Socket, T) error) error` |
| `Event[T]` | `HandleClient(ClientRegistration, func(ctx, Endpoint, T) error) error` |
| `Event[T]` | `Emit(ctx, Endpoint, T) error`; `EmitTo(ctx, BroadcastOperator, T) (BroadcastResult, error)` |
| Root | `NewAckEvent[T,R](name string) AckEvent[T,R]` |
| `AckEvent[T,R]` | `Name() string`; `Handle(*Namespace, func(ctx, *Socket, T) (R,error)) error` |
| `AckEvent[T,R]` | `HandleClient(ClientRegistration, func(ctx, Endpoint, T) (R,error)) error` |
| `AckEvent[T,R]` | `EmitWithAck(ctx, Endpoint, T) (R,error)` |
| `Endpoint` | `SendPacket(ctx, parser.Packet) error`; `RequestAck(ctx, parser.Packet) (Args,error)` |
| `ClientRegistration` | embeds `Endpoint`; `RegisterEvent(string, ClientRawHandler) error` |
| `RawHandler` / `ClientRawHandler` | `func(ctx, *Socket, RawEvent) error` / `func(ctx, Endpoint, RawEvent) error` |
| `RawAck` | `Respond(ctx, Args) error` |
| Root | `Auth[T](func(ctx, *Socket, T) error) Middleware` |
| `Middleware` | `func(ctx, *Socket, json.RawMessage) error` |
| Root | `NewServer(Options) (*Server,error)` |
| `Server` | `Namespace(string) *Namespace`; `Shutdown(ctx) error`; `Close() error` |
| `Namespace` | `Name() string`; `Use(Middleware) error`; `OnRaw(RawHandler) error`; `To(...Room) BroadcastOperator`; `Hooks() *Hooks`; `Logger() *slog.Logger` |
| `Socket` | implements `Endpoint`; `ID() SocketID`; `Join(...Room) error`; `Leave(...Room) error` |
| `BroadcastOperator` | `Except(...Room) BroadcastOperator`; `Local() BroadcastOperator` |
| `Binary` | `SocketIOBinary() []byte` implements `parser.BinaryValue` |
| `parser.ArgumentCodec[T]` | `Encode func(T) (Args,error)`; `Decode func(Args) (T,error)` |
| `Options` | `Normalize() (Options,error)` |
| `client` | `Dial(ctx, string, client.Options) (*Client,error)` |
| `client.Client` | implements `ClientRegistration`; `OnRaw(ClientRawHandler) error`; `Close() error` |

`Args2[A,B]` uses `First A` and `Second B`. `RawEvent` contains `Name`, positional
`Args` and an optional `Ack`; nil means no ack was requested. `parser.Packet`
contains a packet type, namespace, optional `*uint64` ID (zero is valid), lazy JSON
`Data` and binary attachments. `client.Options.Auth` is raw JSON. Public error
identities are listed in [errors.go](errors.go); they are reserved for runtime.

The defaults and units have one source in `Options` godoc. Zero budget fields select
those bounded defaults; negative budgets are rejected. Observer fields instead keep
zero/nil disabled or unresolved. `Options` composes `Logger *slog.Logger`,
`Engine engineio.Options`, `Hooks *Hooks` and `Adapter AdapterFactory`. Engine options
contain only `Logger`, `Hooks`, `PayloadPreviewBytes` and `PayloadRedactor`, not full
transport options. Engine normalization rejects preview limits outside 0..256;
zero stays disabled. Root normalization validates that nested configuration.
Normalization copies values and does not invoke callbacks, resolve inherited
loggers, mutate the application logger, or enforce runtime delivery limits.

## Package graph and TS/JS check

```text
client ──> socketio ──> parser ──> standard library
   │           └─────> engineio ──> engineio/frame, engineio/packet
   └─────────────────> parser             (all leaves use standard library only)
fixtures/externaladapter ──> socketio, parser
fixtures/positive ──> client, socketio, parser, engineio, fixtures/externaladapter
socketio external tests ──> client, socketio, parser, engineio
```

The root never imports `client` or `fixtures/externaladapter`. Parser and Engine.IO
never import root. `go list -deps ./...` and the compiler check the graph is acyclic.
The external adapter is a separate package within this standalone module; the
production adapter will be a separate module, whose dependency and release wiring
remain to be verified. There is no memory adapter implementation in this artifact.

Room exclusion follows the pinned [Socket.IO 4.8.1 broadcast operator](https://github.com/socketio/socket.io/blob/socket.io%404.8.1/packages/socket.io/lib/broadcast-operator.ts):
`Except` selects rooms. Socket IDs can name their automatic room, as in the
[socket implementation](https://github.com/socketio/socket.io/blob/socket.io%404.8.1/packages/socket.io/lib/socket.ts).
This draft uses `type SocketID = Room` to compile the roadmap's exact
`nsp.To("room").Except(s.ID())` example while retaining typed room values. The
Go generic descriptor and error-first typed ack convention come from the approved
Go roadmap; they are not claims that Node imposes those application conventions.
No byte interoperability is claimed: `Args2` expansion, nested binary extraction,
error-first ack conversion and all codecs remain unimplemented.
Descriptors currently retain only their names; `parser.ArgumentCodec[T]` describes
a proposed conversion boundary but is not constructed or bound to descriptors.
Runtime implementation must add that binding with its encoding/decoding tests.

## Integration gate still required

Stage 1b must finish before transplanting this API into the production layout.
Stage 2.0 then atomically removes the legacy root server/client/namespace/handler
runtime, v1-specific tests, temporary `adapter` and `adapter/redis` packages,
compatibility aliases, `Server.Adapter` and redigo dependency while introducing
the production skeleton. Applicable regression scenarios must move to v2 fixtures,
Engine.IO/parser packages must remain buildable, and the entire root module must
build and test before G2. This preparation makes none of those live changes.
Before claiming G2, the contract owner must additionally freeze and compile:

- Final approval of the Adapter/Factory and hook declarations now compiled here;
  resolve the explicitly proposed `RemoteSocket`, flags, option field names and
  preview-redactor boundary described below. The full packet codec contract remains
  missing: packet values and typed conversion shapes do not establish a complete
  stream encoder/decoder boundary.
- Complete result/reason enum definitions and error-to-result classification. The
  roadmap's explicitly listed values are represented; its unspecified string
  domains remain unresolved, as documented below.
- Complete server/client/Engine.IO option composition, logger propagation, lifecycle
  context access, connection callbacks and shutdown ownership signatures.
- Descriptor codec construction and registration wrappers without handler
  reflection, plus the production CI matrix that retains compile-negative fixtures.

The integrator should reconcile the proposed option/field names, `SocketID` alias,
packet/argument ownership, and `Endpoint.RequestAck` raw return contract with the
other isolated branches before a shared interface is declared frozen. Those are
visible integration decisions, not hidden runtime behavior. Runtime stages must
then prove all lifecycle, byte/queue limits, error mapping and Go/Node wire contracts
specified in roadmap 2.0–2.4. An approved preparation PR is not G2 approval.

## Runtime requirements carried forward

The following roadmap requirements are documented for integration, not implemented
or proved by these compile fixtures:

- Namespace CONNECT/auth runs off the session reader with no waiting queue and no
  concurrent attempt for the same namespace. Excess or duplicate attempts return
  CONNECT_ERROR without starting middleware. `MaxConcurrentConnects` and
  `ConnectTimeout` specify its bounded defaults in the Options godoc. A task keeps
  its slot until middleware actually returns, including after cancellation or its
  deadline. Saturated or non-cooperative auth must not block ping or ACK processing.
- ACK IDs increase monotonically, never repeat during an Engine.IO session, and
  remain allocated across namespace reconnects. The maximum is `2^53-1`, compatible
  with JavaScript safe integers. Further requests fail with `ErrAckIDExhausted`
  until a new transport session begins. `parser.Packet.ID` represents the optional
  ID but does not enforce this range; the future allocator and codec must do so.
- Both server and client must test all descriptor/ACK-ID combinations below with
  handler success and failure, in both Go/Node directions, including binary ACKs.
  Responses are queued at most once and only when an incoming ID is present.

| Registered descriptor | Incoming ACK ID | Required runtime behavior |
| --- | --- | --- |
| `Event[T]` | absent | Run handler; send errors to hooks/logs only. |
| `Event[T]` | present | Run handler; reply `[null]` on success or typed error envelope. |
| `AckEvent[T,R]` | absent | Run handler and discard result; errors go to hooks/logs only. |
| `AckEvent[T,R]` | present | Run handler; reply using the documented typed ACK convention. |

## Adapter and observer contract inventory

These additions prepare stages 2.0/2.2/2.4 without passing G2. In the table below
`ctx` is `context.Context`. Hook metadata names mean the types declared in the
respective package, with exactly the fields in roadmap 2.4.

| Owner | Signature |
| --- | --- |
| `Adapter` | `AddAll(SocketID, []Room)`; `Del(SocketID, Room)`; `DelAll(SocketID)` |
| `Adapter` | `Broadcast(ctx, parser.Packet, BroadcastOptions) (BroadcastResult,error)` |
| `Adapter` | `Sockets(ctx, []Room) ([]SocketID,error)`; `SocketRooms(SocketID) []Room` |
| `Adapter` | `FetchSockets(ctx, BroadcastOptions) ([]RemoteSocket,error)` |
| `Adapter` | `ServerSideEmit(ctx, string, ...any) error`; `Close() error` |
| `AdapterFactory` | `func(*Namespace) (Adapter,error)` |
| `BroadcastOptions` | `Rooms, Except []Room`; `Flags BroadcastFlags` |
| Proposed `BroadcastFlags` | `Local bool` |
| Proposed `RemoteSocket` | `ID SocketID`; `Rooms []Room`; `Handshake, Data json.RawMessage` |
| `engineio.Options` | `Normalize() (Options,error)` |
| Proposed `engineio.PayloadRedactor` | `Enabled(ctx) bool`; `Redact(ctx, SessionInfo, PacketInfo, []byte, int) []byte` |

`BroadcastFlags.Local` is a deliberately minimal selection from pinned
[socket.io-adapter 2.5.5](https://github.com/socketio/socket.io-adapter/blob/2.5.5/lib/in-memory-adapter.ts).
The local installed 2.5.5 declarations were checked; Node also has volatile,
compression, broadcast, binary and timeout flags. None of those options or their
runtime semantics are implemented or silently defaulted by this proposal.
`RemoteSocket` preserves the four metadata categories exposed by
[Socket.IO 4.8.1](https://github.com/socketio/socket.io/blob/socket.io%404.8.1/packages/socket.io/lib/broadcast-operator.ts#L438),
but proposes JSON-only handshake/data snapshots instead of live objects, generic
application data or remote methods. Arbitrary non-JSON or binary application data
needs an explicit mapping before G2. Snapshot production must copy rooms and JSON
buffers and define auth/header exposure; declaring these fields supplies neither
copying nor redaction. Snapshot mutation must not mutate an adapter's internal state.

The roadmap Adapter methods are retained exactly, including `ServerSideEmit`'s
variadic arguments. That argument escape hatch is not an untyped user handler.
Server-side emit success means publication acceptance only. No automatic retry,
cluster delivery acknowledgement, remote command API or Node wire compatibility is
implemented by this compile proof. Runtime partial-result and concurrency tests
remain mandatory.

| Engine.IO hook field | Function signature |
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

| Socket.IO hook field | Function signature |
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

The hooks have no runtime fire points, nil-safe dispatch wrapper, context chaining,
`ChainHooks`, or `LoggingHooks` implementation here. `Namespace.Hooks()` and
`Namespace.Logger()` are nil-safe prototype getters returning nil; production
must wire them and provide the roadmap's safe adapter dispatch. All callbacks in
positive fixtures are compiled only. Metadata slices are borrowed for each call;
observers retaining data must copy it. The hook docs state required start/terminal
pairing and context ordering but this slice does not demonstrate either at runtime.

The constants in the two `hooks.go` files enumerate only roadmap 2.4's explicit
value sets: Engine.IO handshake results and close reasons; Socket.IO event,
connect and ACK results; adapter message kinds. Their names are a proposal;
the string values come from the lifecycle/metric tables. Hook fields remain strings
where the roadmap declares strings, so Go does not enforce closed enums.
`ConnectEnd` and `EmitEnd` expose errors rather than result strings; their constants
are intended for future classification, not extra hook fields. Upgrade results,
request-rejection reasons, Socket.IO disconnect reasons, and adapter result mapping
are not fully enumerated in the source plan. G2 must resolve them rather than
implicitly treating arbitrary strings as frozen valid values.

Preview options declare an explicit security contract, without capturing payloads:
limit > 0, a configured trusted redactor, and `Enabled(ctx)` must all hold before
capture. A runtime consumer such as TRACE logging must actually be enabled; TRACE
alone must not turn capture on. The trusted redactor receives transient complete
packet bytes so it can classify protocol content before truncation, returns an
owned bounded preview, and must drop CONNECT/auth and uncertain/incomplete data.
The final runtime must enforce a maximum of 256 bytes, redact before delivery,
never retain a full frame, and test fragmented/auth packets plus callback retention.
Engine.IO does not import the Socket.IO parser. Standalone applications must supply
their own redactor. This proposed two-method interface and full-packet access need
G2 review; no built-in redactor or operational preview gate is supplied here.
