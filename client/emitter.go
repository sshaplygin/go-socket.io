package client

type emitter struct {
	socket     *namespaceConn
	volatile   bool
	noCompress bool
}

func (e emitter) Volatile() Emitter { e.volatile = true; return e }

func (e emitter) Send(args ...interface{}) { e.Emit("message", args...) }

func (e emitter) Emit(event string, args ...interface{}) {
	e.socket.emitFlags(e.volatile, e.noCompress, event, args...)
}

func (nc *namespaceConn) Volatile() Emitter { return (emitter{socket: nc}).Volatile() }

func (e emitter) Compress(on bool) Emitter { e.noCompress = !on; return e }

func (nc *namespaceConn) Compress(on bool) Emitter {
	return (emitter{socket: nc}).Compress(on)
}
