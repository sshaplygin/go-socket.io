package transport

import (
	"io"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

// CompressWriter is the optional per-packet compression extension of a Conn.
// Connections without it use their normal writer for either flag value.
type CompressWriter interface {
	NextWriterCompress(ft frame.Type, pt packet.Type, compress bool) (io.WriteCloser, error)
}
