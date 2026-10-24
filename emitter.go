package socketio

import (
	"reflect"

	"github.com/sshaplygin/go-socket.io/adapter"
)

type emitter struct {
	socket    *namespaceConn
	adapter   adapter.Adapter
	broadcast bool
	opts      adapter.Options
}

func (e emitter) asBroadcast() emitter {
	if !e.broadcast {
		e.broadcast = true
		e.adapter = e.socket.broadcast
		e.opts.Except = append(append([]string(nil), e.opts.Except...), e.socket.ID())
	}
	return e
}

func (e emitter) To(rooms ...string) Emitter {
	e = e.asBroadcast()
	e.opts.Rooms = append(append([]string(nil), e.opts.Rooms...), rooms...)
	return e
}

func (e emitter) Except(ids ...string) Emitter {
	e = e.asBroadcast()
	e.opts.Except = append(append([]string(nil), e.opts.Except...), ids...)
	return e
}

func (e emitter) Volatile() Emitter { e.opts.Volatile = true; return e }

func (e emitter) Local() Emitter { e.opts.Local = true; return e }

func (e emitter) Send(args ...interface{}) error { return e.Emit("message", args...) }

func (e emitter) Emit(event string, args ...interface{}) error {
	if e.broadcast {
		if e.adapter == nil {
			return errServerOnly
		}
		if hasBroadcastAck(args) {
			return ErrBroadcastAck
		}
		return e.adapter.SendTo(e.opts, event, args...)
	}
	e.socket.emitFlags(e.opts.Volatile, e.opts.NoCompress, event, args...)
	return nil
}

func hasBroadcastAck(args []interface{}) bool {
	if len(args) == 0 {
		return false
	}
	t := reflect.TypeOf(args[len(args)-1])
	return t != nil && t.Kind() == reflect.Func
}

func (e emitter) Compress(on bool) Emitter { e.opts.NoCompress = !on; return e }

// newEmitter creates a pending namespace broadcast over its resolved adapter.
func newEmitter(b adapter.Adapter) Emitter { return emitter{adapter: b, broadcast: true} }
