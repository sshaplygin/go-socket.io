# V1 component migration

The v1 component restructuring changes Go imports and API on `master`. Socket.IO v4
and Engine.IO v3 wire behavior stays the same. This guide covers changes already landed
in the component series; the remaining work is tracked in
[ROADMAP.md](ROADMAP.md#stage-v1-work-packages). V1-to-v2 migration has its own guide.

## Engine.IO codecs and polling

| Old import/API | New import/API |
| --- | --- |
| `engineio/frame.Type`, `String`, `Binary`, `ByteToFrameType` | `engineio/parser.FrameType`, `String`, `Binary`, `ByteToFrameType` |
| `engineio/packet.Type`, packet constants and `ByteToPacketType` | `engineio/parser.PacketType`, the same constants and `ByteToPacketType` |
| `engineio/packet.Encoder`, `Decoder`, `NewEncoder`, `NewDecoder` | `engineio/parser.PacketEncoder`, `PacketDecoder`, `NewPacketEncoder`, `NewPacketDecoder` |
| Packet `FrameReader`, `FrameWriter`, `Frame`, `Packet` and test-fake constructors | Same names in `engineio/parser`, using its frame and packet types |
| `transport.ConnParameters`, `transport.ReadConnParameters` | `engineio/parser.ConnParameters`, `engineio/parser.ReadConnParameters`; `WriteTo` unchanged |
| `engineio/payload.Payload`, `New`, feed/flush/pause/deadline methods | Private polling state; use `polling.Transport` through `transport.Conn` |
| `engineio/payload.Error`, `OpError` | `engineio/transport/polling.Error`, `OpError`; same error text and temporary classification |
| `engineio/transport/utils.Timestamp`, `TimestampFromClock` | Same names in `engineio/transport` |

Transport implementations now use `parser.FrameType` and `parser.PacketType` in their
reader/writer methods. The old import directories are removed without forwarding aliases.
For example, a transport writer accepts these shared wire types:

```go
import (
    "io"

    eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

func writeMessage(conn interface {
    NextWriter(eioparser.FrameType, eioparser.PacketType) (io.WriteCloser, error)
}, body []byte) error {
    w, err := conn.NextWriter(eioparser.String, eioparser.MESSAGE)
    if err != nil {
        return err
    }
    _, err = w.Write(body)
    closeErr := w.Close()
    if err != nil {
        return err
    }
    return closeErr
}
```

For standalone streaming payload codecs, implement `parser.PayloadReader` or
`parser.PayloadWriter` and construct `NewPayloadDecoder(source)` or
`NewPayloadEncoder(sink, WithBinarySupport(supportBinary))`. A source lends a complete payload through
`AcquirePayloadReader() (io.Reader, bool, error)`; the bool selects binary framing.
`ReleasePayloadReader(error) error` receives the decode error, or nil after the last
packet reader is closed. A sink lends a writer for each packet through
`AcquirePayloadWriter() (io.Writer, error)` and receives its completion through
`ReleasePayloadWriter(error) error`. An acquisition error does not trigger release;
a release error takes precedence over the codec error. Close every packet reader/writer
before requesting another. A decoder retains decode/release failures: later calls
return that terminal error without reading or releasing the source again. A non-EOF
body read error releases the source immediately, including a read that returns data
and an error together; normal packet EOF remains successful. Acquisition
failures may be retried because they did not acquire a payload. The codecs own framing; polling owns HTTP requests,
concurrency, deadlines and pause/resume. Use godoc for the complete streaming contract.

## Server and client roles

| Old import/API | Current import/API |
| --- | --- |
| Root `Client`, `NewClient`, `ErrEmptyAddr`, `EmptyAddrErr` | `client.Client`, `client.New`, `client.ErrEmptyAddr` |
| `client.NewClient(addr, *engineio.Options)` | `client.New(addr, ...client.Option)` |
| Root connection type/error aliases | Server types/errors belong to `socketio`; client types/errors belong to `client` |
| `engineio.Dialer`, `engineio.Opener` | `engineio/client.Dialer`, `engineio/client.Opener` |
| `session.ReadyState`, `CloseReason`, `CloseError` | Server contracts in `engineio`; client contracts in `engineio/client` |
| `session.FrameType`, `TEXT`, `BINARY` | Engine frame APIs use `engineio/parser.FrameType`, `String`, `Binary`; Socket.IO codec APIs use `parser.FrameType`, `Text`, `Binary` |
| Public `session.New`/`Session`/`Manager` | Server-owned private session/registry; obtain connections through `engineio.Server.Accept`, inspect them through `Clients`/`Count` |
| `session.IDGenerator`, `DefaultIDGenerator` | Same names in `engineio` |
| `session.Pauser` | `engineio/transport.Pauser` |
| Session hook/limit/request setters | `engineio.WithHooks`, `WithMaxHTTPBufferSize`, `WithUpgradeTimeout`; `WithConnInitor` receives the handshake request and connection |
| Concrete session `Abort` | `engineio.Abort(conn)`; custom engines receive ordinary `Close` |
| Concrete session `Shutdown` | Shutdown belongs to `engineio.Server.Close` |

Use `socketio.Conn` in server handlers and `client.Conn` in client handlers. The same
rule applies to `Namespace`, `Emitter`, `AckFunc` and `Handshake`. Their identities are
independent; callbacks using the other role's `Conn` no longer type-check on typed
registrations and are rejected at event registration. `ErrWriteBufferFull` has a separate
identity in each role: use the error of that role with `errors.Is`. `ErrServerOnly` belongs
to the client. Client `EngineConn` and `Request` still return nil, and `Handshake` still
returns its zero value. Room, ACK, buffering and reconnect behavior stays the same.

Server `engineio.Conn` and client `engineio/client.Conn` have separate state/error types.
Use the matching role's constants and `CloseError` with `errors.As`. Both use common
Engine.IO frame types. Socket.IO parser implementations use their own frame types;
convert explicitly to/from `engineio/parser.FrameType` at a transport boundary. The
library's server and client perform these conversions internally.

## Constructor configuration

Default constructors omit optional configuration: `socketio.NewServer()`,
`engineio.NewServer()` and `client.New(addr)`. Each role exposes its own `Option`
functions. Options apply in order; the last setting wins. Reusing options creates
independent instance configuration. Client headers/query and engine cookie/built-in
transport settings retain their construction-time copies. Callback state and custom
transports remain application-owned.

| Previous configuration | Current option |
| --- | --- |
| `NewServer(*engineio.Options)` or `NewServer(*socketio.Options)` | `socketio.NewServer(...socketio.Option)` |
| Root `Options.Engine` | `socketio.WithEngineOptions(...engineio.Option)` |
| Root queue/drain fields | `socketio.WithWriteBufferSize(n)`, `socketio.WithCloseTimeout(d)` |
| Engine logger also used by Socket.IO | `socketio.WithLogger(log)` configures both layers |
| `engineio.NewServer(*engineio.Options)` | `engineio.NewServer(...engineio.Option)` |
| Engine `PingInterval`, `PingTimeout`, `UpgradeTimeout` | `engineio.WithPingInterval(d)`, `WithPingTimeout(d)`, `WithUpgradeTimeout(d)` |
| Engine `MaxHTTPBufferSize`, `CompressionThreshold` | `engineio.WithMaxHTTPBufferSize(n)`, `WithCompressionThreshold(n)` |
| Engine `DisableUpgrades`, `DisableHTTPCompression`, `DisableCORS` | `engineio.WithUpgrades(!disabled)`, `WithHTTPCompression(!disabled)`, `WithCORS(!disabled)` |
| Engine `Cookie`, `DisableCookie` | `engineio.WithCookie(cookie)`, `WithCookieEnabled(!disabled)` |
| Engine `PerMessageDeflate`, `Transports` | `engineio.WithPerMessageDeflate(enabled)`, `WithTransports(transports...)` |
| Engine `SessionIDGenerator`, `RequestChecker`, `ConnInitor`, `Hooks`, `Logger` | `engineio.WithSessionIDGenerator(gen)`, `WithRequestChecker(check)`, `WithConnInitor(init)`, `WithHooks(hooks)`, `WithLogger(log)` |
| `client.New(addr, *client.Options)` | `client.New(addr, ...client.Option)` |
| Client `Path`, `Query`, `Header`, `Timeout` | `client.WithPath(path)`, `WithQuery(query)`, `WithHeader(header)`, `WithTimeout(timeout)` |
| Client `WriteBufferSize`, `CloseTimeout`, `Logger` | `client.WithWriteBufferSize(n)`, `WithCloseTimeout(d)`, `WithLogger(log)` |
| Client `Transports`, `PerMessageDeflate` | `client.WithTransports(transports...)`, `WithPerMessageDeflate(enabled)` |
| Client `DisableReconnection` | `client.WithReconnection(!disabled)` |
| Client retry attempts/delay/cap/jitter | `client.WithReconnectionAttempts(n)`, `WithReconnectionDelay(d)`, `WithReconnectionDelayMax(d)`, `WithRandomizationFactor(factor)` |
| `transport.NewManager(transports)` | `transport.NewManager(transport.WithTransports(transports...))`; `NewManager()` creates an empty registry |
| `parser.NewPayloadEncoder(writer, supportBinary)` in Engine.IO | `parser.NewPayloadEncoder(writer, parser.WithBinarySupport(supportBinary))`; omitted configuration enables binary payload framing |

The public server/client option structs are removed. Required addresses, codec I/O
and fake-reader input frames remain positional. Codec constructors without optional
configuration keep their required reader/writer arguments.

Engine.IO heartbeat timeout and Socket.IO drain duration remain independent. Move any
old `PingTimeout` used as a drain bound to the Socket.IO role's `WithCloseTimeout`.
Nonpositive queue/drain values still mean 64 packets and one minute. Other existing
zero/negative/default semantics are unchanged; each With function documents them.
Root `WithLogger` configures Socket.IO and its engine. A later
`WithEngineOptions(engineio.WithLogger(other))` overrides only the engine's sink.

```go
server := socketio.NewServer(
    socketio.WithEngineOptions(
        engineio.WithPingInterval(25*time.Second),
        engineio.WithPingTimeout(20*time.Second),
    ),
    socketio.WithWriteBufferSize(64),
    socketio.WithCloseTimeout(time.Minute),
)
server.OnConnect("/", func(c socketio.Conn) error {
    c.Join("lobby")
    return nil
})

cl, err := client.New("http://127.0.0.1:8000",
    client.WithWriteBufferSize(64),
    client.WithCloseTimeout(time.Minute),
)
if err != nil {
    return err
}
cl.OnConnect(func(c client.Conn) error { return nil })
```

`engineio.Conn.Close` remains orderly; `engineio.Abort` is immediate. `Server.Remove`
remains a registry operation and does not close its transport. Private session state
is not an application entry point. Configure public server hooks before accepting
connections, and retain request metadata in `ConnInitor` when needed.

## Example error ownership

Examples return startup/serving errors to `main`, which logs and returns after
cleanup. They no longer use process-terminating logger calls. Each server example
owns its HTTP listeners and Socket.IO server, closes both when a service fails or
the signal context is cancelled, and waits for its serving goroutines. The graceful
example preserves its signal delay; service errors bypass it. Framework examples
mount the framework's HTTP handler on the owned `http.Server`, including GoFrame
whose built-in runner can terminate the process. An Engine.IO echo peer error ends
only that peer and releases its acquired readers/writers. The Go client closes on
connect/login errors as well as successful completion.

## Adapters and Redis

| Previous root API | Current API |
| --- | --- |
| `Broadcast`, `BroadcastOptions`, `EachFunc`, `AdapterFunc` | `adapter.Adapter`, `adapter.Options`, `adapter.EachFunc`, `adapter.Factory` |
| Root `ErrExceptUnsupported` | `adapter.ErrExceptUnsupported`; error text and `errors.Is` wrapping are preserved |
| Private memory implementation | `adapter.NewMemory()` |
| `Server.Adapter(*RedisAdapterOptions)` | `socketio.WithAdapter(redis.New(...redis.Option))` at construction, or `Server.SetAdapter(factory)` for later namespaces |
| `RedisAdapterOptions.Addr`, `Prefix`, `Network`, `Password` | `redis.WithAddr`, `WithPrefix`, `WithNetwork`, `WithPassword` |
| Deprecated Redis `Host`, `Port` | `redis.WithHost`, `WithPort`; the default address still takes precedence, so use `WithAddr` |
| Redis `DB` | `redis.WithDB`; the existing DB0 limitation remains |

`redis.New()` returns a factory without connecting to Redis. The old `Server.Adapter`
upfront connection probe is removed: namespace registration applies the factory once,
retains construction errors, and `Server.Serve` reports the first recorded error.
`SetAdapter` affects future namespaces; existing ones keep their adapter. Nil selects
memory. Constructor options are ordered, with the last `WithAdapter` winning.
Shutdown uses optional `io.Closer` and joins completed adapter close errors with the
engine error. Adapters that own resources should implement it.

```go
import (
    socketio "github.com/sshaplygin/go-socket.io"
    "github.com/sshaplygin/go-socket.io/adapter"
    redisadapter "github.com/sshaplygin/go-socket.io/adapter/redis"
)

server := socketio.NewServer(socketio.WithAdapter(redisadapter.New(
    redisadapter.WithAddr("127.0.0.1:6379"),
    redisadapter.WithPrefix("socket.io"),
)))
server.OnConnect("/", func(c socketio.Conn) error { c.Join("lobby"); return nil })
server.ForEach("/", "lobby", func(c adapter.Socket) { c.Emit("news", "hello") })
```

External adapters take `adapter.Socket`, whose methods are `ID`, `Emit` and
`EmitWithOptions`. The last method applies only `Volatile` and `NoCompress` to that
socket; the adapter owns room/exclusion/cluster selection. Root `Conn` implements this
contract; custom sockets and mocks must add the flagged method if they do not embed
it. `ForEach` callbacks needing server-specific metadata can use a checked type assertion
to `socketio.Conn`. `adapter.Memory` releases its index lock before invoking sockets or
callbacks. Redis composes Memory; its legacy JSON format, remote flags, exclusion,
subscription timing and DB0 limitations remain unchanged.

## Private Engine.IO session ownership

The active server API remains `engineio.Conn`, `CompressWriter`, `ReadyState`,
`CloseReason`, `CloseError`, `IDGenerator`, `DefaultIDGenerator` and their constants.
These names now alias canonical server types owned by `engineio/internal/session`.
Applications keep using `engineio` and its functional options; the internal import is
not an application entry point. The retired public `engineio/session` path remains
removed. The Engine.IO client retains its own independent contracts.

Code using the active public types, callback signatures, methods and constants still
compiles. `errors.Is`/`errors.As` and error text remain unchanged; a reflected type's
canonical package path or `%T` spelling can now refer to the private session package.
The server still installs hooks before startup, retains the handshake request, owns
shutdown and exposes snapshots through `Clients`/`Count`. `engineio.Abort` recognizes
only its own concrete session and uses ordinary `Close` for custom engines, including
ones with an unrelated Abort method. No init-time registration or compatibility
session wrapper is introduced.

## Private Engine.IO codec ownership

Keep importing `engineio/parser`. Its active frame/packet types, reader/writer
contracts and packet/payload encoder/decoder types now alias canonical private
codec types. Public constructors and `WithBinarySupport` retain their signatures,
defaults and option ordering. OPEN parameters and the public frame/packet fakes
remain in the public component. The retired public `engineio/frame`, `packet` and
`payload` import paths stay removed.

No application imports `engineio/parser/internal/packet` or `internal/payload`.
Reflection package paths or `%T` output may now name those private codec packages;
source signatures, constant values, exported fields and method sets are retained.
Existing custom frame and payload sources/destinations continue to implement the
public contracts. Streaming lease/error behavior is described above and in godoc.
