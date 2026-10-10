package socketio

import "errors"

// ErrNotImplemented is returned by every operation of the v2 API skeleton that
// needs the runtime. The runtime lands in roadmap stages 2.1 to 2.4; until then no
// operation that returns this error has any effect.
var ErrNotImplemented = errors.New("socketio: not implemented in the v2 API skeleton")

// Sentinel errors of the runtime contract (roadmap 2.3). The skeleton never
// returns them; they are declared so that consumers can match them with
// errors.Is from the first runtime release.
var (
	// ErrNamespaceClosed is returned by Server.Namespace once shutdown has begun, and
	// by a creation whose factory returned after shutdown began.
	ErrNamespaceClosed = errors.New("socketio: namespace closed")

	// ErrAckTimeout ends an emit-with-ack when Options.AckTimeout, not the caller's
	// context, expired.
	ErrAckTimeout = errors.New("socketio: ack timeout")

	// ErrWriteBufferFull reports an outbound queue overflow.
	ErrWriteBufferFull = errors.New("socketio: write buffer full")

	// ErrSocketClosed ends every pending ack when the socket disconnects.
	ErrSocketClosed = errors.New("socketio: socket closed")

	// ErrTooManyPendingAcks rejects an emit-with-ack past Options.MaxPendingAcks.
	ErrTooManyPendingAcks = errors.New("socketio: too many pending acks")

	// ErrMessageTooLarge rejects a message past Options.MaxEventBytes.
	ErrMessageTooLarge = errors.New("socketio: message too large")

	// ErrTooManyAttachments rejects a message past Options.MaxAttachments.
	ErrTooManyAttachments = errors.New("socketio: too many attachments")

	// ErrAckIDExhausted rejects new ack requests once the IDs of an Engine.IO
	// connection reached the JavaScript safe integer maximum (2^53-1).
	ErrAckIDExhausted = errors.New("socketio: ack ID space exhausted")
)
