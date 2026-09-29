# v2 generic API compile proof

This standalone Go 1.22 module prepares part of roadmap stage 2.0 while stages 1
and 1b proceed. It changes no production API, root dependency or CI workflow. Its
module path is experimental; it is not the future `/v2` release module.

**This artifact does not pass G2 or implement Socket.IO runtime.** It establishes
that the proposed descriptor signatures preserve payload, handler and ack types
across a separate client package. Constructors of descriptors, immutable room
selectors, metadata accessors, the binary marker and option normalization work as
pure helpers. Every operation that would register, authenticate, connect, send,
acknowledge or close returns `ErrNotImplemented`. `NewServer` and `client.Dial`
return a nil object and that error. The positive fixtures are compiled, never run.

## Verification

Run inside this directory:

```sh
go test -race -count=1 ./...
go vet ./...
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
server/client positive fixture, then checks 11 deliberately invalid programs:
handler argument/return, ack return, emitted payload, ack request/result,
client handler/ack return, broadcast payload, `Args2` argument order and auth
handler type. Each must fail in `invalid.go` with all recorded diagnostic
fragments. An import/toolchain failure alone cannot pass a negative fixture.
The compiler subprocesses have a one-minute deadline. Other tests check all
runtime stubs report the sentinel and that zero selects bounded defaults.

## Proposed signatures

This inventory describes the implemented preparation slice. `ctx` means
`context.Context`; `Args` means `parser.Arguments`. Full declarations and generic
parameter names are in [event.go](event.go), [model.go](model.go),
[parser/types.go](parser/types.go), [client/client.go](client/client.go) and
[options.go](options.go).

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
| `Namespace` | `Name() string`; `Use(Middleware) error`; `OnRaw(RawHandler) error`; `To(...Room) BroadcastOperator` |
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

The defaults and units have one source in `Options` godoc. Every zero value selects
that bounded default; negative values are rejected. Normalization is a pure copy
operation and does not establish that the future runtime enforces these limits.

## Package graph and TS/JS check

```text
client ──> socketio ──> parser ──> standard library
   └─────────────────> parser
fixtures/positive ──> client, socketio, parser
socketio external tests ──> client, socketio, parser
```

The root never imports `client`. The parser never imports the root. `go list
-deps ./...` and the compiler check the graph is acyclic. Runtime adapters and
hooks are not represented by this graph yet.

Room exclusion follows the pinned [Socket.IO 4.8.1 broadcast operator](https://github.com/socketio/socket.io/blob/socket.io%404.8.1/packages/socket.io/lib/broadcast-operator.ts):
`Except` selects rooms. Socket IDs can name their automatic room, as in the
[socket implementation](https://github.com/socketio/socket.io/blob/socket.io%404.8.1/packages/socket.io/lib/socket.ts).
This draft uses `type SocketID = Room` to compile the roadmap's exact
`nsp.To("room").Except(s.ID())` example while retaining typed room values. The
Go generic descriptor and error-first typed ack convention come from the approved
Go roadmap; they are not claims that Node imposes those application conventions.
No byte interoperability is claimed: `Args2` expansion, nested binary extraction,
error-first ack conversion and all codecs remain unimplemented.

## Integration gate still required

Stage 1b must finish before transplanting this API into the production layout.
Stage 2.0 then atomically removes the legacy root server/client/namespace/handler
runtime, v1-specific tests, temporary `adapter` and `adapter/redis` packages,
compatibility aliases, `Server.Adapter` and redigo dependency while introducing
the production skeleton. Applicable regression scenarios must move to v2 fixtures,
Engine.IO/parser packages must remain buildable, and the entire root module must
build and test before G2. This preparation makes none of those live changes.
Before claiming G2, the contract owner must additionally freeze and compile:

- The complete `Adapter`, `AdapterFactory`, `RemoteSocket`, broadcast flags and
  packet codec contracts; this slice only proposes packet values and typed argument
  conversion shapes, not a complete stream encoder/decoder boundary.
- Both hook structs, closed result/reason enums and their integration into options.
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
