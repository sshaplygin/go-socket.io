package socketio

import (
	"errors"
	"github.com/sshaplygin/go-socket.io/internal/connection"
)

var errServerClosed = errors.New("socketio: server closed")

// ErrWriteBufferFull is reported once, to OnError of the packet's namespace and unordered with
// OnDisconnect, when an Emit or a packet the library queues (an ACK or CONNECT reply) finds the
// outbound queue (WithWriteBufferSize) full before any close started; a packet being
// written does not count. Emit never blocks: that packet and every later one are dropped and the
// connection is closed without draining. An overflow in root OnConnect fails the connect and is
// reported with a nil Conn. Packets queued faster than they are written can close a healthy client:
// polling writes one engine.io frame per round trip, and a packet with k binary attachments takes k+1
// frames.
var ErrWriteBufferFull = connection.ErrWriteBufferFull

// ErrBroadcastAck means a broadcast has a trailing acknowledgement callback.
// Only single-socket emits and the legacy broadcast methods support callbacks.
var ErrBroadcastAck = connection.ErrBroadcastAck
