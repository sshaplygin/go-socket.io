package client

import (
	"cmp"
	"errors"
	"io"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/parser"
)

// queueWriter starts no packet (its TEXT frame) once the queue is discarded. failed is the
// first failure of NextWriter or of the frame writer it returned since the last NextWriter.
type queueWriter struct {
	c          *conn
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
	if compressed, ok := w.c.Conn.(engineclient.CompressWriter); ok {
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
	failed error
}

func (r *frameReader) NextReader() (parser.FrameType, io.ReadCloser, error) {
	ft, fr, err := r.FrameReader.NextReader()
	if r.failed = err; err != nil {
		return parser.FrameType(ft), fr, err
	}
	return parser.FrameType(ft), frameIO{r: fr, c: fr, failed: &r.failed}, nil
}

// frameIO is a frame reader or writer that records its first failure in *failed; io.EOF
// from Read ends the frame. It returns the errors unchanged.
type frameIO struct {
	r      io.Reader
	w      io.Writer
	c      io.Closer
	failed *error
}

func (f frameIO) mark(err error) error {
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

func (c *conn) serveError() {
	defer close(c.errorStopped)
	for {
		select {
		case <-c.closing:
			c.reportOverflow(c.overflow)
			return
		case errMsg := <-c.errorChan:

			if handler := c.namespace(errMsg.namespace); handler != nil {
				if f := handler.ErrorHandler(); f != nil && errMsg.conn != nil {
					f(errMsg.conn, errMsg.err)
				}
			}
			close(errMsg.done)
		}
	}
}

// serveWrite writes until the queue is discarded, or empty after the seal.
func (c *conn) serveWrite() {
	defer close(c.writeStopped)
	for {
		if isDone(c.discard) {
			return
		}
		pkg, ok := c.nextQueued()
		if !ok {
			if isDone(c.seal) {
				// Close the engine client once the application queue has drained.
				// Keep the timer active until the reader ends.
				go func() { _ = c.Conn.Close() }()
				return
			}
			select {
			case <-c.discard:
				return
			case <-c.writeReady:
			case <-c.seal:
			}
			continue
		}
		c.writer.failed = nil
		c.writer.noCompress = pkg.noCompress
		err := pkg.encodeErr
		if err == nil {
			if pkg.prepared != nil {
				err = c.encoder.EncodePrepared(pkg.prepared)
			} else {
				err = c.encoder.Encode(pkg.Header)
			}
		}
		if err != nil {
			c.report(pkg.Header.Namespace, err, c.writer.failed)
			if c.writer.failed != nil {
				c.stop()
			} else {
				c.writer.noCompress = false
				if err := c.encoder.Encode(parser.Header{Type: parser.Error}, "encode error"); err != nil || c.writer.failed != nil {
					c.stop()
				}
			}
		}
	}
}

// serveRead dispatches packets until a close takes the namespaces.
func (c *conn) serveRead(connect, disconnect func(*conn, parser.Header) error) {
	type packetHeader struct {
		header parser.Header
		event  string
		err    error
	}
	headers := make(chan packetHeader)
	consumed, stop, workerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	// Header I/O may block on the transport. Its worker waits for dispatch to
	// finish before touching the decoder again, keeping argument decoding serial.
	go func() {
		defer close(workerDone)
		for {
			var next packetHeader
			next.err = c.decoder.DecodeHeader(&next.header, &next.event)
			select {
			case headers <- next:
			case <-stop:
				return
			}
			if next.err != nil {
				return
			}
			select {
			case <-consumed:
			case <-stop:
				return
			}
		}
	}()
	defer func() {
		close(stop)
		c.finishRead()
		<-workerDone
		close(c.readStopped)
	}()
	for {
		var next packetHeader
		select {
		case <-c.readReady:
			c.runReadTasks()
			continue
		case next = <-headers:
		}
		if next.err != nil {
			c.log.Debug("socketio: header decode failed", "err", next.err)
			c.onError(rootNamespace, next.err)
			return
		}
		header, event := next.header, next.event
		if header.Namespace == aliasRootNamespace {
			header.Namespace = rootNamespace
		}

		var err error
		closed := isDone(c.closing) // then the handlers dispatch nothing and end no drain
		switch header.Type {
		case parser.Ack:
			err = ackPacketHandler(c, header)
		case parser.Connect:
			err = connect(c, header)
		case parser.Disconnect:
			err = disconnect(c, header)
		case parser.Event:
			err = eventPacketHandler(c, event, header)
		case parser.Error:
			err = errorPacketHandler(c, header)
		}

		if err != nil && !closed {
			c.log.Debug("socketio: read stopped", "err", err)

			return
		}
		consumed <- struct{}{}
	}
}
