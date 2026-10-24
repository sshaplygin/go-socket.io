package socketio

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sshaplygin/go-socket.io/internal/event"
)

// Endpoint is the shared sending surface of server and client namespace sockets.
// Typed descriptors support the built-in server connections, clients and client
// namespace sockets. Implementing Emit alone does not support typed sends.
type Endpoint interface{ Emit(string, ...interface{}) }

// ClientRegistration accepts typed handlers on a client or a client namespace.
type ClientRegistration interface {
	RegisterEvent(string, EventHandler) error
}

// The public interfaces keep the method sets of the typed event contracts, and
// server connections are endpoints.
var (
	_ Endpoint                 = event.Endpoint(nil)
	_ event.Endpoint           = Endpoint(nil)
	_ ClientRegistration       = event.ClientRegistration(nil)
	_ event.ClientRegistration = ClientRegistration(nil)
	_ Endpoint                 = (*namespaceConn)(nil)
)

// EventHandler is an opaque registration created by a typed event descriptor.
// Its zero value is invalid. Valid reports whether a descriptor created it, and
// WantsAck whether that descriptor is an AckEvent. Decode and Invoke are library
// bridge methods using internal payload types.
type EventHandler = event.Handler

// RemoteError carries the peer's error-first acknowledgement code and message.
// *RemoteError implements error; Error returns Code + ": " + Message.
type RemoteError = event.RemoteError

var (
	// ErrInvalidEventName rejects empty names and reserved lifecycle event names.
	ErrInvalidEventName = event.ErrInvalidEventName
	// ErrDuplicateEvent means a typed registration would replace an existing listener.
	ErrDuplicateEvent = event.ErrDuplicateEvent
	// ErrUnsupportedEndpoint means the endpoint lacks typed sending support.
	ErrUnsupportedEndpoint = event.ErrUnsupportedEndpoint
	// ErrInvalidPayload means an event does not contain exactly one value of its type.
	ErrInvalidPayload = event.ErrInvalidPayload
	// ErrInvalidAck means the peer sent an invalid error-first acknowledgement.
	ErrInvalidAck = event.ErrInvalidAck
	// ErrAckTimeout ends an acknowledgement wait after the internal 30-second limit.
	ErrAckTimeout = event.ErrAckTimeout
	// ErrSocketClosed means the request's namespace generation has ended.
	ErrSocketClosed = event.ErrSocketClosed
)

// Event describes an event carrying exactly one value of type T. Structs and
// slices remain one argument; nested parser.Buffer values are binary attachments.
// The same descriptor can be registered and emitted by both Socket.IO roles.
type Event[T any] struct{ name string }

// NewEvent creates a descriptor. Registration and sending validate its name.
func NewEvent[T any](name string) Event[T] { return Event[T]{name: name} }

// Name returns the event name supplied to NewEvent.
func (e Event[T]) Name() string { return e.name }

// Handle atomically registers a server handler, rejecting an occupied name.
// The context ends with this namespace generation, not the handshake request.
// Returned errors and recovered panics reach OnError without closing the socket.
func (e Event[T]) Handle(nsp *ServerNamespace, h func(context.Context, Conn, T) error) error {
	if err := event.ValidateName(e.name); err != nil {
		return err
	}
	if nsp == nil || nsp.server == nil {
		return ErrUnsupportedEndpoint
	}
	if h == nil {
		return fmt.Errorf("%w: nil handler", ErrInvalidPayload)
	}
	return nsp.registration().RegisterEvent(e.name, event.NewHandler(event.SchemaOf[T](), false,
		func(ctx context.Context, to event.Endpoint, payload event.Payload) (event.Payload, error) {
			value, err := event.Value[T](payload)
			if err != nil {
				return event.Payload{}, err
			}
			conn, ok := to.(Conn)
			if !ok {
				return event.Payload{}, ErrUnsupportedEndpoint
			}
			return event.Payload{}, h(ctx, conn, value)
		}))
}

// HandleClient registers on a client or client namespace. The callback endpoint
// is the actual client connection of that namespace generation.
func (e Event[T]) HandleClient(c ClientRegistration, h func(context.Context, Endpoint, T) error) error {
	if err := event.ValidateName(e.name); err != nil {
		return err
	}
	if event.IsNil(c) {
		return ErrUnsupportedEndpoint
	}
	if h == nil {
		return fmt.Errorf("%w: nil handler", ErrInvalidPayload)
	}
	return c.RegisterEvent(e.name, event.NewHandler(event.SchemaOf[T](), false,
		func(ctx context.Context, to event.Endpoint, payload event.Payload) (event.Payload, error) {
			value, err := event.Value[T](payload)
			if err != nil {
				return event.Payload{}, err
			}
			return event.Payload{}, h(ctx, to, value)
		}))
}

// Emit snapshots v and returns an encoding or queue-admission error. Success
// means local admission, not remote delivery. Cancellation before admission sends
// nothing; client pre-connect buffering retains the context until admission.
func (e Event[T]) Emit(ctx context.Context, to Endpoint, v T) error {
	if err := event.ValidateName(e.name); err != nil {
		return err
	}
	ctx = event.Context(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if event.IsNil(to) {
		return ErrUnsupportedEndpoint
	}
	target, ok := to.(event.TypedEndpoint)
	if !ok {
		return ErrUnsupportedEndpoint
	}
	return target.SendEvent(ctx, e.name, event.ValueOf(v))
}

// EmitTo broadcasts one payload through an existing immutable Emitter. It
// checks cancellation before invoking the emitter and preserves adapter errors
// and partial-delivery behavior. Success does not count remote delivery.
func (e Event[T]) EmitTo(ctx context.Context, to Emitter, v T) error {
	if err := event.ValidateName(e.name); err != nil {
		return err
	}
	if err := event.Context(ctx).Err(); err != nil {
		return err
	}
	if event.IsNil(to) {
		return ErrUnsupportedEndpoint
	}
	return to.Emit(e.name, v)
}

// AckEvent describes a single payload T answered by a single result R. The wire
// ACK is [null, result] on success or [{"code":"...","message":"..."}] on error.
type AckEvent[T, R any] struct{ name string }

// NewAckEvent creates an acknowledged event descriptor.
func NewAckEvent[T, R any](name string) AckEvent[T, R] { return AckEvent[T, R]{name: name} }

// Name returns the event name supplied to NewAckEvent.
func (e AckEvent[T, R]) Name() string { return e.name }

// Handle registers a server handler and acknowledges only packets requesting an
// ACK. Application errors become handler_error; RemoteError preserves its code.
func (e AckEvent[T, R]) Handle(nsp *ServerNamespace, h func(context.Context, Conn, T) (R, error)) error {
	if err := event.ValidateName(e.name); err != nil {
		return err
	}
	if nsp == nil || nsp.server == nil {
		return ErrUnsupportedEndpoint
	}
	if h == nil {
		return fmt.Errorf("%w: nil handler", ErrInvalidPayload)
	}
	return nsp.registration().RegisterEvent(e.name, event.NewHandler(event.SchemaOf[T](), true,
		func(ctx context.Context, to event.Endpoint, payload event.Payload) (event.Payload, error) {
			value, err := event.Value[T](payload)
			if err != nil {
				return event.Payload{}, err
			}
			conn, ok := to.(Conn)
			if !ok {
				return event.Payload{}, ErrUnsupportedEndpoint
			}
			result, err := h(ctx, conn, value)
			return event.ValueOf(result), err
		}))
}

// HandleClient registers an acknowledged handler on a client or client namespace.
func (e AckEvent[T, R]) HandleClient(c ClientRegistration, h func(context.Context, Endpoint, T) (R, error)) error {
	if err := event.ValidateName(e.name); err != nil {
		return err
	}
	if event.IsNil(c) {
		return ErrUnsupportedEndpoint
	}
	if h == nil {
		return fmt.Errorf("%w: nil handler", ErrInvalidPayload)
	}
	return c.RegisterEvent(e.name, event.NewHandler(event.SchemaOf[T](), true,
		func(ctx context.Context, to event.Endpoint, payload event.Payload) (event.Payload, error) {
			value, err := event.Value[T](payload)
			if err != nil {
				return event.Payload{}, err
			}
			result, err := h(ctx, to, value)
			return event.ValueOf(result), err
		}))
}

// EmitWithAck waits for the peer's error-first response, bounded by ctx and an
// internal 30-second timeout including pre-connect buffering. Nil ctx means
// context.Background. Caller cancellation returns ctx.Err; the internal bound
// returns ErrAckTimeout. Disconnect terminates the wait with ErrSocketClosed.
// Run waits outside callbacks on the same connection: its serialized dispatcher
// cannot process the ACK until the callback returns.
func (e AckEvent[T, R]) EmitWithAck(ctx context.Context, to Endpoint, v T) (R, error) {
	return e.emitWithAck(ctx, to, v, 30*time.Second)
}

func (e AckEvent[T, R]) emitWithAck(ctx context.Context, to Endpoint, v T, timeout time.Duration) (R, error) {
	var zero R
	if err := event.ValidateName(e.name); err != nil {
		return zero, err
	}
	ctx = event.Context(ctx)
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if event.IsNil(to) {
		return zero, ErrUnsupportedEndpoint
	}
	target, ok := to.(event.TypedEndpoint)
	if !ok {
		return zero, ErrUnsupportedEndpoint
	}
	wait, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	payload, err := target.RequestAck(wait, e.name, event.ValueOf(v), event.SchemaOf[R]())
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if ctx.Err() != nil {
				return zero, ctx.Err()
			}
			if wait.Err() != nil {
				return zero, ErrAckTimeout
			}
		}
		return zero, err
	}
	return event.Value[R](payload)
}
