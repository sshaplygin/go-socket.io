package connection

import (
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"sync"

	namespaceImpl "github.com/sshaplygin/go-socket.io/internal/namespace"
	"github.com/sshaplygin/go-socket.io/parser"
)

var emtpyFH = namespaceImpl.NewAckFunc(func() {})

func ackPacketHandler(c *Connection, header parser.Header) error {
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

	handler, ok := rawFunc.(*namespaceImpl.Func)
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

func eventPacketHandler(c *Connection, event string, header parser.Header) error {
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
	var types []reflect.Type
	if len(snapshot) != 0 {
		types = snapshot[0].ArgTypes()
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

	if err := conn.runMiddleware(event, args); err != nil {
		c.write(parser.Header{Type: parser.Error, Namespace: header.Namespace}, reflect.ValueOf(err.Error()))
		return nil
	}
	var once sync.Once
	ack := namespaceImpl.AckFunc(func(values ...interface{}) {
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
	ret, ownsAck, err := handler.DispatchListeners(conn, event, snapshot, ack, args...)
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

func connectPacketHandler(c *Connection, header parser.Header) error {
	if err := c.decoder.DiscardLast(); err != nil {
		c.onError(header.Namespace, err)
		c.log.Debug("socketio: connect discard failed", nspAttr(header.Namespace), "err", err)
		return nil
	}

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

func disconnectPacketHandler(c *Connection, header parser.Header) error {
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

func errorPacketHandler(c *Connection, header parser.Header) error {
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
