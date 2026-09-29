// Package socketio is an isolated compile proof for the proposed v2 API.
// Runtime operations return ErrNotImplemented. This is not a usable Socket.IO library.
package socketio

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sshaplygin/go-socket.io/experiments/v2-api/parser"
)

// ErrNotImplemented identifies every runtime operation in this compile proof.
var ErrNotImplemented = errors.New("v2 API compile proof: runtime not implemented")

// Endpoint is shared by server sockets and the separate client package.
// Runtime sends must copy encoded data before returning and preserve message groups.
type Endpoint interface {
	SendPacket(context.Context, parser.Packet) error
	RequestAck(context.Context, parser.Packet) (parser.Arguments, error)
}

// ClientRegistration avoids importing client into the root API.
// Raw callbacks are an explicit escape hatch, not untyped generic handlers.
type ClientRegistration interface {
	Endpoint
	RegisterEvent(string, ClientRawHandler) error
}

type ClientRawHandler func(context.Context, Endpoint, RawEvent) error
type RawHandler func(context.Context, *Socket, RawEvent) error

// RawAck responds using the application's chosen positional ack convention.
type RawAck interface {
	Respond(context.Context, parser.Arguments) error
}

// RawEvent exposes positional JSON arguments, attachments and an optional responder.
// Ack is nil when the peer did not request an acknowledgement.
type RawEvent struct {
	Name string
	Args parser.Arguments
	Ack  RawAck
}

// Args2 expands to two positional arguments; ordinary slices remain one argument.
// Expansion is a codec obligation and is not implemented in this compile proof.
type Args2[A, B any] struct {
	First  A
	Second B
}

// Binary marks bytes for attachment encoding, including nested values.
type Binary []byte

func (b Binary) SocketIOBinary() []byte { return []byte(b) }

var _ parser.BinaryValue = Binary(nil)

// Event keeps the payload/handler type relationship on the descriptor.
type Event[T any] struct {
	name string
}

func NewEvent[T any](name string) Event[T] { return Event[T]{name: name} }
func (e Event[T]) Name() string            { return e.name }
func (e Event[T]) Handle(*Namespace, func(context.Context, *Socket, T) error) error {
	return ErrNotImplemented
}
func (e Event[T]) HandleClient(ClientRegistration, func(context.Context, Endpoint, T) error) error {
	return ErrNotImplemented
}
func (e Event[T]) Emit(context.Context, Endpoint, T) error { return ErrNotImplemented }
func (e Event[T]) EmitTo(context.Context, BroadcastOperator, T) (BroadcastResult, error) {
	return BroadcastResult{}, ErrNotImplemented
}

// AckEvent binds both request and response types. Runtime uses error-first ACKs:
// success [null, ...resultArgs], failure [{"code":"...","message":"..."}].
type AckEvent[T, R any] struct {
	name string
}

func NewAckEvent[T, R any](name string) AckEvent[T, R] { return AckEvent[T, R]{name: name} }
func (e AckEvent[T, R]) Name() string                  { return e.name }
func (e AckEvent[T, R]) Handle(*Namespace, func(context.Context, *Socket, T) (R, error)) error {
	return ErrNotImplemented
}
func (e AckEvent[T, R]) HandleClient(ClientRegistration, func(context.Context, Endpoint, T) (R, error)) error {
	return ErrNotImplemented
}
func (e AckEvent[T, R]) EmitWithAck(context.Context, Endpoint, T) (R, error) {
	var zero R
	return zero, ErrNotImplemented
}

// Middleware is namespace authentication applied before acceptance.
type Middleware func(context.Context, *Socket, json.RawMessage) error

// Auth preserves the credential handler type without reflection over handlers.
func Auth[T any](func(context.Context, *Socket, T) error) Middleware {
	return func(context.Context, *Socket, json.RawMessage) error { return ErrNotImplemented }
}
