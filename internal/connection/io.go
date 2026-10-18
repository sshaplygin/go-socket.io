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
	defer c.finishRead()

	var event string

	for {
		var header parser.Header

		if err := c.decoder.DecodeHeader(&header, &event); err != nil {
			c.log.Debug("socketio: header decode failed", "err", err)
			c.onError(rootNamespace, err)
			return
		}

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
	}
}
