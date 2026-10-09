package socketio

import "errors"

// Runtime error identities required by stage 2.3. They are not emitted by the
// compile proof, whose runtime operations always return ErrNotImplemented.
var (
	ErrNamespaceClosed    = errors.New("namespace closed")
	ErrAckTimeout         = errors.New("ack timeout")
	ErrWriteBufferFull    = errors.New("write buffer full")
	ErrSocketClosed       = errors.New("socket closed")
	ErrTooManyPendingAcks = errors.New("too many pending acks")
	ErrMessageTooLarge    = errors.New("message too large")
	ErrTooManyAttachments = errors.New("too many attachments")
	ErrAckIDExhausted     = errors.New("ack ID space exhausted")
)
