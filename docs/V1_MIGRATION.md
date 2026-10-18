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
`NewPayloadEncoder(sink, supportBinary)`. A source lends a complete payload through
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
| `client.NewClient(addr, *engineio.Options)` | `client.New(addr, *client.Options)` |
| Root connection type/error aliases | Server types/errors belong to `socketio`; client types/errors belong to `client` |
| `engineio.Dialer`, `engineio.Opener` | `engineio/client.Dialer`, `engineio/client.Opener` |
| `session.ReadyState`, `CloseReason`, `CloseError` | Server contracts in `engineio`; client contracts in `engineio/client` |
| `session.FrameType`, `TEXT`, `BINARY` | Engine frame APIs use `engineio/parser.FrameType`, `String`, `Binary`; Socket.IO codec APIs use `parser.FrameType`, `Text`, `Binary` |
| Public `session.New`/`Session`/`Manager` | Server-owned private session/registry; obtain connections through `engineio.Server.Accept`, inspect them through `Clients`/`Count` |
| `session.IDGenerator`, `DefaultIDGenerator` | Same names in `engineio` |
| `session.Pauser` | `engineio/transport.Pauser` |
| Session hook/limit/request setters | `engineio.Options.Hooks`, `MaxHTTPBufferSize`, `UpgradeTimeout`; `ConnInitor` receives the handshake request and connection |
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

`socketio.NewServer` now accepts `*socketio.Options`. Move old Engine.IO settings into
its `Engine` field. Move the Socket.IO queue size to `WriteBufferSize` and explicitly
set `CloseTimeout` when the old `PingTimeout` also configured drain duration. Engine.IO
heartbeat `PingTimeout` is independent of the Socket.IO drain bound. Client queue/drain
settings belong to `client.Options`; its transport, header, reconnect and logger settings
also remain there. Nonpositive queue/drain values still mean 64 packets and one minute.

```go
server := socketio.NewServer(&socketio.Options{
    Engine: &engineio.Options{
        PingInterval: 25 * time.Second,
        PingTimeout: 20 * time.Second,
    },
    WriteBufferSize: 64,
    CloseTimeout: time.Minute,
})
server.OnConnect("/", func(c socketio.Conn) error {
    c.Join("lobby")
    return nil
})

cl, err := client.New("http://127.0.0.1:8000", &client.Options{
    WriteBufferSize: 64,
    CloseTimeout: time.Minute,
})
if err != nil {
    return err
}
cl.OnConnect(func(c client.Conn) error { return nil })
```

`engineio.Conn.Close` remains orderly; `engineio.Abort` is immediate. `Server.Remove`
remains a registry operation and does not close its transport. Private session state
is not an application entry point. Configure public server hooks before accepting
connections, and retain request metadata in `ConnInitor` when needed.
