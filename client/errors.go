package client

import (
	"errors"
)

var errUnavailableRootHandler = errors.New("root ('/') doesn't have a namespace handler")
var errFailedConnectNamespace = errors.New("failed connect to namespace without handler")
var errHandleDispatch = errors.New("handler dispatch error")
var errDecodeArgs = errors.New("decode args error")

// ErrWriteBufferFull is reported once, to OnError of the packet's namespace and unordered with
// OnDisconnect, when an Emit or a packet the library queues (an ACK or CONNECT reply) finds the
// outbound queue (WithWriteBufferSize) full before any close started; a packet being
// written does not count. Emit never blocks: that packet and every later one are dropped and the
// connection is closed without draining. An overflow in root OnConnect fails the connect and is
// reported with a nil Conn. Packets queued faster than they are written can close a healthy client:
// polling writes one engine.io frame per round trip, and a packet with k binary attachments takes k+1
// frames.
var ErrWriteBufferFull = errors.New("write buffer full")

// ErrServerOnly means a client connection cannot perform a server broadcast.
var ErrServerOnly = errors.New("socketio: server-only method on a client connection")

type errorMessage struct {
	namespace string
	conn      *namespaceConn // nil if namespace has no OnError; see conn.errConn
	done      chan struct{}  // closed once OnError returned

	err error
}

func newErrorMessage(namespace string, err error) *errorMessage {
	return &errorMessage{
		namespace: namespace,
		done:      make(chan struct{}),
		err:       err,
	}
}
