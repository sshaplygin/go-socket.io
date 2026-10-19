// Package event holds the shared typed-event mechanics without owning either
// Socket.IO role or its connection lifecycle.
package event

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/sshaplygin/go-socket.io/parser"
)

var (
	ErrInvalidEventName    = errors.New("socketio: invalid typed event name")
	ErrDuplicateEvent      = errors.New("socketio: event already registered")
	ErrUnsupportedEndpoint = errors.New("socketio: endpoint does not support typed events")
	ErrInvalidPayload      = errors.New("socketio: invalid typed payload")
	ErrInvalidAck          = errors.New("socketio: invalid typed acknowledgement")
	ErrAckTimeout          = errors.New("socketio: acknowledgement timeout")
	ErrSocketClosed        = errors.New("socketio: namespace socket closed")
)

// Endpoint is the common sending surface of existing server and client sockets.
type Endpoint interface{ Emit(string, ...interface{}) }

// ClientRegistration accepts an opaque typed event handler.
type ClientRegistration interface{ RegisterEvent(string, Handler) error }

// TypedEndpoint is implemented by built-in sockets in addition to Endpoint.
type TypedEndpoint interface {
	SendEvent(context.Context, string, Payload) error
	RequestAck(context.Context, string, Payload, Schema) (Payload, error)
}

// Payload preserves a value's declared type, including a nil interface value.
type Payload struct{ value reflect.Value }

// Schema describes one positional argument without exposing reflection publicly.
type Schema struct{ typ reflect.Type }

func SchemaOf[T any]() Schema        { return Schema{typ: reflect.TypeFor[T]()} }
func ValueOf[T any](value T) Payload { return Payload{value: reflect.ValueOf(&value).Elem()} }
func Value[T any](payload Payload) (T, error) {
	var zero T
	if !payload.value.IsValid() || payload.value.Type() != reflect.TypeFor[T]() {
		return zero, ErrInvalidPayload
	}
	// A nil interface cannot be type asserted, but is still a valid typed value.
	if payload.value.Kind() == reflect.Interface && payload.value.IsNil() {
		return zero, nil
	}
	return payload.value.Interface().(T), nil
}
func (p Payload) Interface() interface{} {
	if !p.value.IsValid() {
		return nil
	}
	return p.value.Interface()
}
func (p Payload) Valid() bool { return p.value.IsValid() }

// Handler retains schema and ACK ownership alongside the application adapter.
// Its zero value is invalid and cannot be registered.
type Handler struct {
	schema Schema
	ack    bool
	invoke func(context.Context, Endpoint, Payload) (Payload, error)
}

func NewHandler(schema Schema, ack bool, invoke func(context.Context, Endpoint, Payload) (Payload, error)) Handler {
	return Handler{schema: schema, ack: ack, invoke: invoke}
}
func (h Handler) Valid() bool                               { return h.schema.typ != nil && h.invoke != nil }
func (h Handler) WantsAck() bool                            { return h.ack }
func (h Handler) Decode(d *parser.Decoder) (Payload, error) { return Decode(d, h.schema) }
func (h Handler) Invoke(ctx context.Context, to Endpoint, payload Payload) (result Payload, err error) {
	defer func() {
		if value := recover(); value != nil {
			err = &RemoteError{Code: "handler_panic", Message: fmt.Sprint(value)}
		}
	}()
	if !h.Valid() {
		return Payload{}, ErrInvalidPayload
	}
	return h.invoke(ctx, to, payload)
}

func Decode(d *parser.Decoder, schema Schema) (Payload, error) {
	if schema.typ == nil {
		_ = d.DiscardLast()
		return Payload{}, ErrInvalidPayload
	}
	values, err := d.DecodeArgsVariadic([]reflect.Type{reflect.SliceOf(schema.typ)})
	if err != nil {
		return Payload{}, fmt.Errorf("%w: %w", ErrInvalidPayload, err)
	}
	if len(values) != 1 {
		return Payload{}, fmt.Errorf("%w: expected one argument, got %d", ErrInvalidPayload, len(values))
	}
	return Payload{value: values[0]}, nil
}

// RemoteError is an error-first acknowledgement returned by the remote handler.
type RemoteError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RemoteError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

// AckArgs encodes the same error-first envelope for both Socket.IO roles.
func AckArgs(value Payload, err error) []interface{} {
	if err == nil {
		return []interface{}{nil, value.Interface()}
	}
	var remote *RemoteError
	if errors.As(err, &remote) && remote != nil && remote.Code != "" {
		return []interface{}{RemoteError{Code: remote.Code, Message: remote.Message}}
	}
	return []interface{}{RemoteError{Code: "handler_error", Message: err.Error()}}
}

// DecodeAck validates the full envelope while decoding the result directly to R.
func DecodeAck(d *parser.Decoder, result Schema) (Payload, error) {
	if result.typ == nil {
		_ = d.DiscardLast()
		return Payload{}, ErrInvalidAck
	}
	values, err := d.DecodeArgsVariadic([]reflect.Type{reflect.TypeFor[interface{}](), reflect.SliceOf(result.typ)})
	if err != nil {
		return Payload{}, fmt.Errorf("%w: %w", ErrInvalidAck, err)
	}
	if len(values) == 2 && values[0].IsNil() {
		return Payload{value: values[1]}, nil
	}
	if len(values) == 1 && !values[0].IsNil() {
		if object, ok := values[0].Interface().(map[string]interface{}); ok && len(object) == 2 {
			code, codeOK := object["code"].(string)
			message, messageOK := object["message"].(string)
			if codeOK && code != "" && messageOK {
				return Payload{}, &RemoteError{Code: code, Message: message}
			}
		}
	}
	return Payload{}, ErrInvalidAck
}

func ValidateName(name string) error {
	switch name {
	case "", "connect", "disconnect", "disconnecting", "error":
		return ErrInvalidEventName
	default:
		return nil
	}
}

func IsNil(value interface{}) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func Context(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
