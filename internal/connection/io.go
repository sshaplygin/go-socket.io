package connection

import (
	"errors"

	"github.com/sshaplygin/go-socket.io/parser"
)

func (c *Connection) serveError() {
	for {
		select {
		case <-c.closing:
			c.reportOverflow(c.overflow)
			return
		case err := <-c.errorChan:
			var errMsg *errorMessage
			if !errors.As(err, &errMsg) {
				continue
			}

			if handler := c.namespace(errMsg.namespace); handler != nil {
				if onError := handler.ErrorHandler(); onError != nil && errMsg.conn != nil {
					onError(errMsg.conn, errMsg.err)
				}
			}
			close(errMsg.done)
		}
	}
}

// serveWrite writes until the queue is discarded, or empty after the seal.
func (c *Connection) serveWrite() {
	for {
		if isDone(c.discard) {
			return
		}
		pkg, ok := c.nextQueued()
		if !ok {
			if isDone(c.seal) {
				// A drained application close lets polling send Engine.IO CLOSE.
				// Keep the drain timer active until the reader ends: its abort
				// must still interrupt an engine waiting for the next poll.
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
		err := pkg.err
		if err == nil {
			err = c.encoder.EncodePrepared(pkg.prepared)
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
func (c *Connection) serveRead(connect, disconnect func(*Connection, parser.Header) error) {
	type packetHeader struct {
		header parser.Header
		event  string
		err    error
	}
	headers := make(chan packetHeader)
	consumed, stop, workerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	// Header I/O may wait indefinitely. Keep admission tasks on the dispatcher
	// while its worker reads only one header, then waits for argument dispatch.
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
		// Interrupt a worker blocked in transport I/O before reading its final
		// failure state and running disconnect callbacks.
		c.stop()
		<-workerDone
		c.finishRead()
	}()
	for {
		if err := c.runReconnects(); err != nil {
			return
		}
		var next packetHeader
		select {
		case <-c.readReady:
			continue
		case next = <-headers:
		}
		if next.err != nil {
			c.log.Debug("socketio: header decode failed", "err", next.err)
			c.onError(rootNamespace, next.err)
			return
		}
		// A ready replacement takes priority over the header already read. Its
		// OnConnect must finish before any event for that replacement is dispatched.
		if err := c.runReconnects(); err != nil {
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
		select {
		case consumed <- struct{}{}:
		case <-stop:
			return
		}
	}
}
