package socketio

import (
	"reflect"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/parser"
)

type preparedBroadcast struct {
	payload outboundPayload
	ack     *callbackFunc
	event   string
	local   []interface{}
}

// PrepareEmit is the memory adapter's optional prepare-once bridge.
func (nc *namespaceConn) PrepareEmit(opts adapter.Options, name string, args ...interface{}) (interface{}, error) {
	packet := &preparedBroadcast{event: name}
	if reservedEvent(name) {
		packet.local = args
		return packet, nil
	}
	if n := len(args); n > 0 && args[n-1] != nil && reflect.TypeOf(args[n-1]).Kind() == reflect.Func {
		packet.ack = newAckFunc(args[n-1])
		args = args[:n-1]
	}
	values := make([]reflect.Value, len(args)+1)
	values[0] = reflect.ValueOf(name)
	for i, arg := range args {
		values[i+1] = reflect.ValueOf(arg)
	}
	header := parser.Header{Type: parser.Event, Namespace: fmtNS(nc.namespace)}
	packet.payload = preparePayload(header, opts.NoCompress, values...)
	// Keep encoding errors in the queue so every recipient retains its usual
	// OnError behavior, including errors from application marshalers.
	return packet, nil
}

// EmitPrepared admits a packet produced by this implementation of PrepareEmit.
func (nc *namespaceConn) EmitPrepared(opts adapter.Options, value interface{}) {
	packet, ok := value.(*preparedBroadcast)
	if !ok {
		return
	}
	if reservedEvent(packet.event) {
		nc.emitLocal(packet.event, packet.local...)
		return
	}
	if opts.Volatile {
		nc.sendVolatile(packet.payload, packet.ack)
	} else {
		nc.sendPayload(packet.payload, packet.ack)
	}
}
