package socketio

import (
	"errors"
)

var errServerClosed = errors.New("socketio: server closed")

// ErrWriteBufferFull is reported once, to OnError of the packet's namespace and unordered with
// OnDisconnect, when an Emit or a packet the library queues (an ACK or CONNECT reply) finds the
// outbound queue (WithWriteBufferSize) full before any close started; a packet being
// written does not count. Queue admission does not wait for free capacity: that packet
// and every later one are dropped, and the connection is closed without draining.
// Emit encodes arguments synchronously and may invoke application marshalers.
// An overflow in root OnConnect fails the connect and is reported with a nil Conn.
// Packets queued faster than they are written can close a healthy client:
// a packet with k binary attachments takes k+1 Engine.IO frames, and transport
// throughput can be lower than the application's emit rate.
var ErrWriteBufferFull = errors.New("write buffer full")

// ErrBroadcastAck means a broadcast has a trailing acknowledgement callback.
// Single-socket emits support callbacks. BroadcastToRoom and BroadcastToNamespace
// register callbacks per socket with the memory adapter; Redis rejects callbacks
// before any delivery and logs the rejection at WARN.
var ErrBroadcastAck = errors.New("socketio: callbacks are not supported when broadcasting")
