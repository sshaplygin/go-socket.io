package transport

import (
	"io"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// CompressWriter is the optional per-packet compression extension of a Conn.
// Connections without it use their normal writer for either flag value.
type CompressWriter interface {
	// NextWriterCompress is FrameWriter.NextWriter with a compression
	// preference for the packet. False sends it uncompressed; true allows the
	// transport's compression, permessage-deflate on websocket or the
	// gzip/deflate response encoding on polling, from the transport's
	// threshold on. A polling response carries a batch of packets and is
	// compressed or not as a whole.
	NextWriterCompress(ft eioparser.FrameType, pt eioparser.PacketType, compress bool) (io.WriteCloser, error)
}
