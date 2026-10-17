package client

import (
	"encoding/json"
	"errors"
	"github.com/sshaplygin/go-socket.io/parser"
	"reflect"
	"sync"
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
		c.log.Warn("socketio: invalid ack function", nspAttr(header.Namespace), "ack_id", header.ID)
		handler = emtpyFH // keep going
	}

	// Read the body because Ack can have body as well
	types := handler.argTypes
	if types == nil {
		types = []reflect.Type{}
	}
	args, err := c.decoder.DecodeArgs(types)
	if err != nil {
		c.log.Debug("socketio: ack decode failed", nspAttr(header.Namespace), "err", err)
		c.onError(header.Namespace, err)
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return nil
		}
		return errDecodeArgs
	}

	// Return value is ignored
	_, err = handler.Call(args)
	if err != nil {
		c.log.Debug("socketio: ack handler failed", nspAttr(header.Namespace), "err", err)
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
		c.log.Warn("socketio: event without handler", nspAttr(header.Namespace))
		return nil
	}

	snapshot := handler.listeners(event)
	var types []reflect.Type
	if len(snapshot) != 0 {
		types = snapshot[0].handler.argTypes
		if types == nil {
			types = []reflect.Type{}
		}
	}
	args, err := c.decoder.DecodeArgs(types)
	if err != nil {
		c.onError(header.Namespace, err)
		c.log.Debug("socketio: event decode failed", nspAttr(header.Namespace), "event", event, "err", err)
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return nil
		}
		return errDecodeArgs
	}

	var once sync.Once
	ack := AckFunc(func(values ...interface{}) {
		if !header.NeedAck {
			return
		}
		once.Do(func() {
			reply := header
			reply.Type = parser.Ack
			args := make([]reflect.Value, len(values))
			for i, value := range values {
				args[i] = reflect.ValueOf(value)
			}
			c.write(reply, args...)
		})
	})
	ret, ownsAck, err := handler.dispatchListeners(conn, event, snapshot, ack, args...)
	if err != nil {
		c.onError(header.Namespace, err)
		c.log.Debug("socketio: event handler failed", nspAttr(header.Namespace), "event", event, "err", err)
		return errHandleDispatch
	}

	if !ownsAck && (len(ret) > 0 || header.NeedAck) {
		header.Type = parser.Ack
		c.write(header, ret...)
	}

	return nil
}

func errorPacketHandler(c *conn, header parser.Header) error {
	args, err := c.decoder.DecodeArgs([]reflect.Type{reflect.TypeOf((*interface{})(nil)).Elem()})
	if err != nil {
		c.onError(header.Namespace, err)
		return errDecodeArgs
	}
	value := args[0].Interface()
	message, ok := value.(string)
	if !ok {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		message = string(encoded)
	}
	c.onError(header.Namespace, errors.New(message))
	return nil
}

func clientConnectPacketHandler(c *conn, header parser.Header) error {
	if err := c.decoder.DiscardLast(); err != nil {
		c.log.Debug("socketio: connect discard failed", nspAttr(header.Namespace), "err", err)
		c.onError(header.Namespace, err)
		return nil
	}

	handler, ok := c.handlers.Get(header.Namespace)
	if !ok {
		c.log.Debug("socketio: connect without handler", nspAttr(header.Namespace))
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
		c.log.Debug("socketio: connect handler failed", nspAttr(header.Namespace), "err", err)
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
		c.log.Debug("socketio: disconnect handler failed", nspAttr(header.Namespace), "err", err)
		c.onError(header.Namespace, err)
		return errHandleDispatch
	}

	return nil
}
