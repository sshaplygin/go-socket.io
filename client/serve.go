package client

import (
	"github.com/sshaplygin/go-socket.io/parser"
)

func (c *conn) serveError() {
	defer close(c.errorStopped)
	for {
		select {
		case <-c.closing:
			c.reportOverflow(c.overflow)
			return
		case errMsg := <-c.errorChan:

			if handler := c.namespace(errMsg.namespace); handler != nil {
				if f := handler.errorHandler(); f != nil && errMsg.conn != nil {
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
		var err error
		switch pkg.Header.Type {
		case parser.Connect, parser.Disconnect:
			err = c.encoder.Encode(pkg.Header)
		case parser.Error:
			if len(pkg.Data) > 0 {
				err = c.encoder.Encode(pkg.Header, pkg.Data[0])
			} else {
				err = c.encoder.Encode(pkg.Header)
			}
		default:
			err = c.encoder.Encode(pkg.Header, pkg.Data)
		}
		if err != nil {
			c.report(pkg.Header.Namespace, err, c.writer.failed)
			if c.writer.failed != nil {
				c.stop()
			} else if err := c.encoder.Encode(parser.Header{Type: parser.Error}, "encode error"); err != nil || c.writer.failed != nil {
				c.stop()
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
