package connection

import (
	"cmp"
	"errors"
	"io"
	"sync"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/parser"
)

// queueWriter starts no packet (its TEXT frame) once the queue is discarded. failed is the
// first failure of NextWriter or of the frame writer it returned since the last NextWriter.
type queueWriter struct {
	c          *Connection
	failed     error
	noCompress bool
}

func (w *queueWriter) NextWriter(ft parser.FrameType) (io.WriteCloser, error) {
	if ft == parser.Text && isDone(w.c.discard) {
		w.failed = io.EOF
		return nil, io.EOF
	}
	var fw io.WriteCloser
	var err error
	if compressed, ok := w.c.Conn.(engineio.CompressWriter); ok {
		fw, err = compressed.NextWriterCompress(eioparser.FrameType(ft), !w.noCompress)
	} else {
		fw, err = w.c.Conn.NextWriter(eioparser.FrameType(ft))
	}
	if w.failed = err; err != nil {
		return nil, err
	}
	return frameIO{w: fw, c: fw, failed: &w.failed}, nil
}

// frameReader is queueWriter's counterpart for the decoder.
type frameReader struct {
	FrameReader interface {
		NextReader() (eioparser.FrameType, io.ReadCloser, error)
	}
	failed    error
	failureMu sync.Mutex
}

func (r *frameReader) NextReader() (parser.FrameType, io.ReadCloser, error) {
	ft, fr, err := r.FrameReader.NextReader()
	r.failureMu.Lock()
	r.failed = err
	r.failureMu.Unlock()
	if err != nil {
		return parser.FrameType(ft), fr, err
	}
	return parser.FrameType(ft), frameIO{r: fr, c: fr, failed: &r.failed, failedMu: &r.failureMu}, nil
}

// frameIO is a frame reader or writer that records its first failure in *failed; io.EOF
// from Read ends the frame. It returns the errors unchanged.
type frameIO struct {
	r        io.Reader
	w        io.Writer
	c        io.Closer
	failed   *error
	failedMu *sync.Mutex
}

func (r *frameReader) failure() error {
	r.failureMu.Lock()
	defer r.failureMu.Unlock()
	return r.failed
}

func (f frameIO) mark(err error) error {
	if f.failedMu != nil {
		f.failedMu.Lock()
		defer f.failedMu.Unlock()
	}
	*f.failed = cmp.Or(*f.failed, err)
	return err
}

func (f frameIO) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if err == io.EOF {
		return n, err
	}
	return n, f.mark(err)
}

func (f frameIO) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	return n, f.mark(err)
}

// expected tells whether err is the frame failure failed, which the decoder or encoder
// returned: an error that follows a frame failure the parser ignored is not.
func expected(err, failed error) bool { return failed != nil && errors.Is(err, failed) }
