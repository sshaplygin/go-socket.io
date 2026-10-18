package connection

import (
	"errors"
	"fmt"
)

// connect errors.
var (
	errUnavailableRootHandler = errors.New("root ('/') doesn't have a namespace handler")

	errFailedConnectNamespace = errors.New("failed connect to namespace without handler")
)

// common connection dispatch errors.
var (
	errHandleDispatch = errors.New("handler dispatch error")

	errDecodeArgs = errors.New("decode args error")
)

// ErrWriteBufferFull is reported once, to OnError of the packet's namespace and unordered with
// OnDisconnect, when an Emit or a packet the library queues (an ACK or CONNECT reply) finds the
// outbound queue (WithWriteBufferSize) full before any close started; a packet being
// written does not count. Emit never blocks: that packet and every later one are dropped and the
// connection is closed without draining. An overflow in root OnConnect fails the connect and is
// reported with a nil namespaceImpl.Conn. Packets queued faster than they are written can close a healthy client:
// polling writes one engine.io frame per round trip, and a packet with k binary attachments takes k+1
// frames.
var ErrWriteBufferFull = errors.New("write buffer full")

// ErrBroadcastAck means a broadcast has a trailing acknowledgement callback.
// Only single-socket emits and the legacy broadcast methods support callbacks.
var ErrBroadcastAck = errors.New("socketio: callbacks are not supported when broadcasting")

var errServerOnly = errors.New("socketio: server-only method on a client connection")

type errorMessage struct {
	namespace string
	conn      *namespaceConn // nil if namespace has no OnError; see conn.errConn
	done      chan struct{}  // closed once OnError returned

	err error
}

func (e errorMessage) Error() string {
	return fmt.Sprintf("error in namespace: (%s) with error: (%s)", e.namespace, e.err.Error())
}

func newErrorMessage(namespace string, err error) *errorMessage {
	return &errorMessage{
		namespace: namespace,
		done:      make(chan struct{}),
		err:       err,
	}
}
