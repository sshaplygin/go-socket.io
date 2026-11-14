package client

import "reflect"

// localEvent reports whether the client keeps an event name local instead of
// sending it: the names socket.io-client 2.x and 1.x keep local, and the
// lifecycle name disconnecting, which the server role reserves.
func localEvent(name string) bool {
	switch name {
	case "connect", "connect_error", "connect_timeout", "connecting",
		"disconnect", "disconnecting", "error",
		"reconnect", "reconnect_attempt", "reconnect_error", "reconnect_failed", "reconnecting",
		"ping", "pong":
		return true
	}
	return false
}

// emitLocal calls this namespace's listeners of a local event name without
// changing the connection. A local emit made while one of this Conn runs is
// dropped, so a listener cannot recurse.
func (nc *namespaceConn) emitLocal(event string, args ...interface{}) {
	if !nc.localDispatch.CompareAndSwap(false, true) {
		return
	}
	defer nc.localDispatch.Store(false)
	handler := nc.conn.namespace(fmtNS(nc.namespace))
	if handler == nil {
		return
	}
	values := make([]reflect.Value, len(args))
	for i, arg := range args {
		if arg == nil {
			values[i] = reflect.Zero(reflect.TypeOf((*interface{})(nil)).Elem())
		} else {
			values[i] = reflect.ValueOf(arg)
		}
	}
	if _, err := handler.DispatchEvent(nc, event, values...); err != nil {
		if onError := handler.ErrorHandler(); onError != nil {
			onError(nc, err)
		} else {
			nc.conn.unhandled(nc.namespace, err)
		}
	}
}
