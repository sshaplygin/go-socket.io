package socketio

import (
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"sync"

	eventImpl "github.com/sshaplygin/go-socket.io/internal/event"

	"github.com/sshaplygin/go-socket.io/parser"
)

var emtpyFH = newAckFunc(func() {})

func ackPacketHandler(c *connection, header parser.Header) error {
	nc, ok := c.namespaces.Get(header.Namespace)
	if !ok {
		_ = c.decoder.DiscardLast()
		return nil
	}

	if raw, exists := nc.ack.Load(header.ID); exists {
		if pending, typed := raw.(*typedPending); typed {
			payload, err := eventImpl.DecodeAck(c.decoder, pending.schema)
			nc.completeTyped(header.ID, pending, payload, err)
			return nil
		}
	}
	defer nc.ack.Delete(header.ID)

	rawFunc, ok := nc.ack.Load(header.ID)
	if !ok {
		// No function for this ack, but still need to read body
		rawFunc = emtpyFH
	}

	handler, ok := rawFunc.(*callbackFunc)
	if !ok {
		// This should never get here and would be solved with generic sync.Map
		c.log.Warn("socketio: invalid ack function", nspAttr(header.Namespace), "ack_id", header.ID)
		handler = emtpyFH // keep going
	}

	// Read the body because Ack can have body as well
	types := handler.ArgTypes()
	if types == nil {
		types = []reflect.Type{}
	}
	args, err := decodeCallbackArgs(c.decoder, types, handler.Variadic())
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

func eventPacketHandler(c *connection, event string, header parser.Header) error {
	if reservedEvent(event) {
		return c.decoder.DiscardLast()
	}
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

	snapshot := handler.Listeners(event)
	if len(snapshot) != 0 {
		if typed, ok := snapshot[0].TypedHandler(); ok {
			return typedEventPacketHandler(c, conn, event, header, typed)
		}
	}
	var types []reflect.Type
	if len(snapshot) != 0 {
		types = snapshot[0].ArgTypes()
		if types == nil {
			types = []reflect.Type{}
		}
	}
	args, err := decodeCallbackArgs(c.decoder, types, len(snapshot) > 0 && snapshot[0].Variadic())
	if err != nil {
		c.onError(header.Namespace, err)
		c.log.Debug("socketio: event decode failed", nspAttr(header.Namespace), "event", event, "err", err)
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return nil
		}
		return errDecodeArgs
	}

	if err := conn.runMiddleware(event, args); err != nil {
		c.write(parser.Header{Type: parser.Error, Namespace: header.Namespace}, reflect.ValueOf(err.Error()))
		return nil
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
			conn.writeAck(reply, args...)
		})
	})
	ret, ownsAck, err := handler.DispatchListeners(conn, event, snapshot, ack, args...)
	if err != nil {
		c.onError(header.Namespace, err)
		c.log.Debug("socketio: event handler failed", nspAttr(header.Namespace), "event", event, "err", err)
		return errHandleDispatch
	}

	if !ownsAck && header.NeedAck && len(snapshot) != 0 {
		header.Type = parser.Ack
		conn.writeAck(header, ret...)
	}

	return nil
}

func connectPacketHandler(c *connection, header parser.Header) error {
	if err := c.decoder.DiscardLast(); err != nil {
		c.onError(header.Namespace, err)
		c.log.Debug("socketio: connect discard failed", nspAttr(header.Namespace), "err", err)
		return nil
	}

	return connectNamespace(c, header)
}

func connectNamespace(c *connection, header parser.Header) error {
	c.mu.Lock()
	if _, waiting := c.retiring[header.Namespace]; waiting && !isDone(c.closing) {
		copy := header
		c.retiring[header.Namespace] = &copy
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	if c.rootRejected != nil || isDone(c.closing) {
		return nil
	}

	handler, ok := c.handlers.Get(header.Namespace)
	if !ok && c.resolveNamespace != nil {
		query, _ := url.ParseQuery(header.Query)
		handler = c.resolveNamespace(header.Namespace, query)
		ok = handler != nil
	}
	if !ok {
		c.onError(header.Namespace, errFailedConnectNamespace)
		c.log.Debug("socketio: connect without handler", nspAttr(header.Namespace))
		c.write(parser.Header{Type: parser.Error, Namespace: header.Namespace}, reflect.ValueOf("Invalid namespace"))
		return nil
	}

	if handler.Err() != nil && !isDone(c.closing) { // its Redis broadcast could not be created
		c.log.Debug("socketio: connect without broadcast", nspAttr(header.Namespace), "err", handler.Err())
		c.connectRecord(header.Namespace, handler.Err())
		c.onError(header.Namespace, handler.Err())
		return errHandleDispatch
	}

	if c.rootRejected != nil || isDone(c.closing) {
		return nil
	}
	if _, exists := c.namespaces.Get(header.Namespace); exists {
		return nil
	}
	nc := newNamespaceConn(c, header.Namespace, handler.Adapter(), header.Query)
	nc.beginConnect()
	nc.SetContext(c.Conn.Context())
	if !c.register(header.Namespace, nc) {
		return nil
	}
	err := handler.Connect(nc)
	nc.connectReturned(err)
	c.connectRecord(header.Namespace, err)
	if err != nil {
		if isDone(c.closing) {
			return nil
		}
		c.claim(header.Namespace)
		nc.reject()
		c.onError(header.Namespace, err)
		c.write(parser.Header{Type: parser.Error, Namespace: header.Namespace}, reflect.ValueOf(err.Error()))
		return nil
	}
	if isDone(c.closing) {
		return nil
	}
	nc.acceptPending()
	return nil
}

func disconnectPacketHandler(c *connection, header parser.Header) error {
	if err := c.decoder.DiscardLast(); err != nil {
		c.onError(header.Namespace, err)
		return errDecodeArgs
	}
	nc, ok := c.claimDisconnect(header.Namespace)
	if !ok {
		return nil
	}
	c.disconnectNamespace(nc, clientDisconnectMsg)
	c.log.Debug("socketio: disconnect", nspAttr(header.Namespace), "reason", "namespace disconnect")
	return nil
}

func errorPacketHandler(c *connection, header parser.Header) error {
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

func reservedEvent(event string) bool {
	switch event {
	case "connect", "disconnect", "disconnecting", "error":
		return true
	}
	return false
}

func decodeCallbackArgs(decoder *parser.Decoder, types []reflect.Type, variadic bool) ([]reflect.Value, error) {
	if variadic {
		return decoder.DecodeArgsVariadic(types)
	}
	return decoder.DecodeArgs(types)
}

func typedEventPacketHandler(c *connection, nc *namespaceConn, name string, header parser.Header, handler eventImpl.Handler) error {
	payload, err := handler.Decode(c.decoder)
	decodeFailed := err != nil
	if err == nil {
		value := payload.Interface()
		if middlewareErr := nc.runMiddleware(name, []reflect.Value{reflect.ValueOf(&value).Elem()}); middlewareErr != nil {
			c.write(parser.Header{Type: parser.Error, Namespace: header.Namespace}, reflect.ValueOf(middlewareErr.Error()))
			return nil
		}
	}
	var result eventImpl.Payload
	if err == nil {
		result, err = handler.Invoke(nc.lifetime, nc, payload)
	}
	if handler.WantsAck() && header.NeedAck {
		header.Type = parser.Ack
		ackErr := err
		if decodeFailed {
			ackErr = &eventImpl.RemoteError{Code: "invalid_payload", Message: err.Error()}
		}
		values := eventImpl.AckArgs(result, ackErr)
		args := make([]reflect.Value, len(values))
		for i, value := range values {
			args[i] = reflect.ValueOf(value)
		}
		nc.writeAck(header, args...)
	}
	if err != nil {
		c.onError(header.Namespace, err)
	}
	return nil
}
