package transport

import (
	"io"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// CompressWriter is the optional per-packet compression extension of a Conn.
// Connections without it use their normal writer for either flag value.
type CompressWriter interface {
	NextWriterCompress(ft eioparser.FrameType, pt eioparser.PacketType, compress bool) (io.WriteCloser, error)
}
