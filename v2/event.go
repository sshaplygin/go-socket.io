package socketio

import (
	"context"

	"github.com/sshaplygin/go-socket.io/v2/parser"
)

// Args2 is an event payload that expands to exactly two positional arguments. An
// ordinary T, including a struct or slice, is one positional JSON argument.
// Expansion is a codec obligation of the runtime.
type Args2[A, B any] struct {
	First  A
	Second B
}

// Binary marks bytes that travel as a binary attachment, including when nested in
// a struct, slice or map payload.
type Binary []byte

// SocketIOBinary implements parser.BinaryValue.
func (b Binary) SocketIOBinary() []byte { return []byte(b) }

var _ parser.BinaryValue = Binary(nil)

// Event describes an event whose payload type is T. The descriptor, not an untyped
// namespace method, keeps the payload and handler types related.
type Event[T any] struct {
	name string
}

// NewEvent returns the descriptor of the event called name.
func NewEvent[T any](name string) Event[T] { return Event[T]{name: name} }

// Name returns the event name.
func (e Event[T]) Name() string { return e.name }

// Handle registers h on nsp. The runtime returns an error for a duplicate
// registration. The skeleton always returns ErrNotImplemented.
func (e Event[T]) Handle(nsp *Namespace, h func(context.Context, *Socket, T) error) error {
	return ErrNotImplemented
}

// HandleClient registers h on a client in place of a server namespace. The
// skeleton always returns ErrNotImplemented.
func (e Event[T]) HandleClient(c ClientRegistration, h func(context.Context, Endpoint, T) error) error {
	return ErrNotImplemented
}

// Emit sends v to one endpoint. The skeleton always returns ErrNotImplemented.
func (e Event[T]) Emit(ctx context.Context, to Endpoint, v T) error { return ErrNotImplemented }

// EmitTo broadcasts v to the selection b. The result counts local enqueues and
// broker acceptance, never remote delivery. The skeleton always returns
// ErrNotImplemented.
func (e Event[T]) EmitTo(ctx context.Context, b BroadcastOperator, v T) (BroadcastResult, error) {
	return BroadcastResult{}, ErrNotImplemented
}

// AckEvent describes an event with payload type T that is answered with a value of
// type R. The runtime acknowledges with the error-first convention: success is
// [null, ...resultArgs] and failure is [{"code": "...", "message": "..."}].
type AckEvent[T, R any] struct {
	name string
}

// NewAckEvent returns the descriptor of the acknowledged event called name.
func NewAckEvent[T, R any](name string) AckEvent[T, R] { return AckEvent[T, R]{name: name} }

// Name returns the event name.
func (e AckEvent[T, R]) Name() string { return e.name }

// Handle registers h on nsp. The runtime returns an error for a duplicate
// registration. The skeleton always returns ErrNotImplemented.
func (e AckEvent[T, R]) Handle(nsp *Namespace, h func(context.Context, *Socket, T) (R, error)) error {
	return ErrNotImplemented
}

// HandleClient registers h on a client in place of a server namespace. The
// skeleton always returns ErrNotImplemented.
func (e AckEvent[T, R]) HandleClient(c ClientRegistration, h func(context.Context, Endpoint, T) (R, error)) error {
	return ErrNotImplemented
}

// EmitWithAck sends v and waits for the peer's answer. The skeleton returns the
// zero R and ErrNotImplemented.
func (e AckEvent[T, R]) EmitWithAck(ctx context.Context, to Endpoint, v T) (R, error) {
	var zero R
	return zero, ErrNotImplemented
}
