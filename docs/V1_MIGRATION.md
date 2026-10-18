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
before requesting another. The codecs own framing; polling owns HTTP requests,
concurrency, deadlines and pause/resume. Use godoc for the complete streaming contract.
