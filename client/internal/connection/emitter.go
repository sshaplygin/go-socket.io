package connection

import (
	"github.com/sshaplygin/go-socket.io/client/internal/namespace"
)

type emitter struct {
	socket              *namespaceConn
	broadcast, volatile bool
	noCompress          bool
}

func (e emitter) To(rooms ...string) namespace.Emitter { e.broadcast = true; return e }

func (e emitter) Except(ids ...string) namespace.Emitter { e.broadcast = true; return e }

func (e emitter) Volatile() namespace.Emitter { e.volatile = true; return e }

func (e emitter) Local() namespace.Emitter { return e }

func (e emitter) Send(args ...interface{}) error { return e.Emit("message", args...) }

func (e emitter) Emit(event string, args ...interface{}) error {
	if e.broadcast {
		return ErrServerOnly
	}
	if socket := e.socket.socket(); socket != nil {
		socket.emitFlags(e.volatile, e.noCompress, event, args...)
	} else {
		e.socket.emitFlags(e.volatile, e.noCompress, event, args...)
	}
	return nil
}

func (nc *namespaceConn) To(rooms ...string) namespace.Emitter {
	return (emitter{socket: nc}).To(rooms...)
}

func (nc *namespaceConn) Broadcast() namespace.Emitter { return (emitter{socket: nc, broadcast: true}) }

func (nc *namespaceConn) Volatile() namespace.Emitter { return (emitter{socket: nc}).Volatile() }

func (e emitter) Compress(on bool) namespace.Emitter { e.noCompress = !on; return e }

func (nc *namespaceConn) Compress(on bool) namespace.Emitter {
	return (emitter{socket: nc}).Compress(on)
}
