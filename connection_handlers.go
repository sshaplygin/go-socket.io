package socketio

import (
	"github.com/sshaplygin/go-socket.io/parser"
)

var emtpyFH = newAckFunc(func() {})

func ackPacketHandler(c *conn, header parser.Header) error {
	nc, ok := c.namespaces.Get(header.Namespace)
	if !ok {
		_ = c.decoder.DiscardLast()
		return nil
	}

	defer nc.ack.Delete(header.ID)

	rawFunc, ok := nc.ack.Load(header.ID)
	if !ok {
		// No function for this ack, but still need to read body
		rawFunc = emtpyFH
	}

	handler, ok := rawFunc.(*funcHandler)
	if !ok {
		// This should never get here and would be solved with generic sync.Map
		c.log.Info("incorrect ack function type", "namespace", header.Namespace, "id", header.ID)
		handler = emtpyFH // keep going
	}

	// Read the body because Ack can have body as well
	args, err := c.decoder.DecodeArgs(handler.argTypes)
	if err != nil {
		c.log.Info("decode ack args", "namespace", header.Namespace, "argTypes", handler.argTypes, "err", err)
		c.onError(header.Namespace, err)
		return errDecodeArgs
	}

	// Return value is ignored
	_, err = handler.Call(args)
	if err != nil {
		c.log.Info("ack handler", "namespace", header.Namespace, "err", err)
		c.onError(header.Namespace, err)
		return errHandleDispatch
	}

	return nil
}

func eventPacketHandler(c *conn, event string, header parser.Header) error {
	conn, ok := c.namespaces.Get(header.Namespace)
	if !ok {
		_ = c.decoder.DiscardLast()
		return nil
	}

	handler, ok := c.handlers.Get(header.Namespace)
	if !ok {
		_ = c.decoder.DiscardLast()
		c.log.Info("missing handler for namespace", "namespace", header.Namespace)
		return nil
	}

	args, err := c.decoder.DecodeArgs(handler.getEventTypes(event))
	if err != nil {
		c.onError(header.Namespace, err)
		c.log.Info("decode event args", "namespace", header.Namespace, "event", event, "argTypes", handler.getEventTypes(event), "err", err)
		return errDecodeArgs
	}

	ret, err := handler.dispatchEvent(conn, event, args...)
	if err != nil {
		c.onError(header.Namespace, err)
		c.log.Info("event handler", "namespace", header.Namespace, "event", event, "err", err)
		return errHandleDispatch
	}

	if len(ret) > 0 || header.NeedAck {
		header.Type = parser.Ack
		c.write(header, ret...)
	}

	return nil
}

func connectPacketHandler(c *conn, header parser.Header) error {
	if err := c.decoder.DiscardLast(); err != nil {
		c.onError(header.Namespace, err)
		c.log.Info("discard connect packet body", "namespace", header.Namespace, "err", err)
		return nil
	}

	handler, ok := c.handlers.Get(header.Namespace)
	if !ok {
		c.onError(header.Namespace, errFailedConnectNamespace)
		c.log.Info("connect to namespace without handler", "namespace", header.Namespace)
		return errFailedConnectNamespace
	}

	conn, ok := c.namespaces.Get(header.Namespace)
	if !ok {
		conn = newNamespaceConn(c, header.Namespace, handler.broadcast)
		if !c.register(header.Namespace, conn) {
			return nil // a close started
		}
		conn.Join(c.Conn.ID())
	}

	_, err := handler.dispatch(conn, header)
	if err != nil {
		c.log.Error("dispatch connect packet", "namespace", header.Namespace, "err", err)
		c.onError(header.Namespace, err)
		return errHandleDispatch
	}

	c.write(header)

	return nil
}

func disconnectPacketHandler(c *conn, header parser.Header) error {
	args, err := c.decoder.DecodeArgs(defaultHeaderType)
	if err != nil {
		c.onError(header.Namespace, err)
		return errDecodeArgs
	}

	conn, ok := c.claim(header.Namespace) // a close may have taken it
	if !ok {
		_ = c.decoder.DiscardLast()
		return nil
	}

	conn.LeaveAll()

	handler, ok := c.handlers.Get(header.Namespace)
	if !ok {
		return nil
	}

	_, err = handler.dispatch(conn, header, args...)
	if err != nil {
		c.log.Error("dispatch disconnect packet", "namespace", header.Namespace, "err", err)
		c.onError(header.Namespace, err)
		return errHandleDispatch
	}

	return nil
}

// ////////////////////
// Client
// ////////////////////

func clientConnectPacketHandler(c *conn, header parser.Header) error {
	if err := c.decoder.DiscardLast(); err != nil {
		c.log.Info("discard connect packet body", "namespace", header.Namespace, "err", err)
		c.onError(header.Namespace, err)
		return nil
	}

	handler, ok := c.handlers.Get(header.Namespace)
	if !ok {
		c.log.Info("connect to namespace without handler", "namespace", header.Namespace)
		c.onError(header.Namespace, errFailedConnectNamespace)
		return errFailedConnectNamespace
	}

	conn, ok := c.namespaces.Get(header.Namespace)
	if !ok {
		conn = newNamespaceConn(c, header.Namespace, handler.broadcast)
		if !c.register(header.Namespace, conn) {
			return nil // a close started
		}
		conn.Join(c.Conn.ID())
	}

	_, err := handler.dispatch(conn, header)
	if err != nil {
		c.log.Error("dispatch connect packet", "namespace", header.Namespace, "err", err)
		c.onError(header.Namespace, err)
		return errHandleDispatch
	}

	return nil
}

func clientDisconnectPacketHandler(c *conn, header parser.Header) error {
	args, err := c.decoder.DecodeArgs(defaultHeaderType)
	if err != nil {
		c.onError(header.Namespace, err)
		return errDecodeArgs
	}

	conn, ok := c.claim(header.Namespace) // a close may have taken it
	if !ok {
		_ = c.decoder.DiscardLast()
		return nil
	}

	conn.LeaveAll()

	handler, ok := c.handlers.Get(header.Namespace)
	if !ok {
		return nil
	}

	_, err = handler.dispatch(conn, header, args...)
	if err != nil {
		c.log.Error("dispatch disconnect packet", "namespace", header.Namespace, "err", err)
		c.onError(header.Namespace, err)
		return errHandleDispatch
	}

	return nil
}
