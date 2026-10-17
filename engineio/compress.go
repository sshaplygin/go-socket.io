package engineio

import (
	"io"

	"github.com/sshaplygin/go-socket.io/engineio/session"
)

// CompressWriter optionally writes packets with a per-packet compression flag.
// NextWriter on these connections is NextWriterCompress with compress true.
type CompressWriter interface {
	NextWriterCompress(ft session.FrameType, compress bool) (io.WriteCloser, error)
}
