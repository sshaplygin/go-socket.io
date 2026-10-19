package client

import "github.com/sshaplygin/go-socket.io/client/internal/connection"

// ErrWriteBufferFull is reported once, to OnError of the packet's namespace and unordered with
// OnDisconnect, when an Emit or a packet the library queues (an ACK or CONNECT reply) finds the
// outbound queue (WithWriteBufferSize) full before any close started; a packet being
// written does not count. Emit never blocks: that packet and every later one are dropped and the
// connection is closed without draining. An overflow in root OnConnect fails the connect and is
// reported with a nil Conn. Packets queued faster than they are written can close a healthy client:
// a packet with k binary attachments takes k+1 Engine.IO frames, and transport
// throughput can be lower than the application's emit rate.
var ErrWriteBufferFull = connection.ErrWriteBufferFull

// ErrServerOnly means a client connection cannot perform a server broadcast.
var ErrServerOnly = connection.ErrServerOnly

// ErrConnectTimeout means the engine connection did not open within WithTimeout.
var ErrConnectTimeout = connection.ErrConnectTimeout
