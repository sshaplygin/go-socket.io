package client

import "errors"

// ErrWriteBufferFull is reported once, to OnError of the packet's namespace and unordered with
// OnDisconnect, when an Emit or a packet the library queues (for example, an ACK) finds the
// outbound queue (WithWriteBufferSize) full before any close started; a packet being
// written does not count. Queue admission does not wait for free capacity: that packet
// and every later one are dropped, and the connection is closed without draining.
// Emit encodes arguments synchronously and may invoke application marshalers.
// An overflow in OnConnect is reported with that namespace's Conn and closes the
// Engine.IO connection; it does not change the result of Connect.
// Packets queued faster than they are written can close a healthy client:
// a packet with k binary attachments takes k+1 Engine.IO frames, and transport
// throughput can be lower than the application's emit rate.
var ErrWriteBufferFull = errors.New("client: write buffer full")

// ErrConnectTimeout means the engine connection did not open within WithTimeout.
var ErrConnectTimeout = errors.New("client: connect timeout")
