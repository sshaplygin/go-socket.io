package socketio

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/internal/event"
	"github.com/stretchr/testify/require"
)

type typedTestEndpoint struct {
	handler event.Handler
	name    string
	payload event.Payload
	request func(context.Context, string, event.Payload, event.Schema) (event.Payload, error)
}

func (*typedTestEndpoint) Emit(string, ...interface{}) {}
func (e *typedTestEndpoint) SendEvent(ctx context.Context, name string, payload event.Payload) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.name, e.payload = name, payload
	return nil
}
func (e *typedTestEndpoint) RequestAck(ctx context.Context, name string, p event.Payload, s event.Schema) (event.Payload, error) {
	return e.request(ctx, name, p, s)
}
func (e *typedTestEndpoint) RegisterEvent(name string, h event.Handler) error {
	e.name, e.handler = name, h
	return nil
}

type legacyTestEndpoint struct{}

func (legacyTestEndpoint) Emit(string, ...interface{}) {}

// Covers T1
func TestTypedEventDescriptor(t *testing.T) {
	e := NewEvent[int]("value")
	require.Equal(t, "value", e.Name())
	target := &typedTestEndpoint{}
	var called int
	require.NoError(t, e.HandleClient(target, func(ctx context.Context, endpoint Endpoint, value int) error {
		require.NotNil(t, ctx)
		require.Same(t, target, endpoint)
		called = value
		return nil
	}))
	_, err := target.handler.Invoke(context.Background(), target, event.ValueOf(42))
	require.NoError(t, err)
	require.Equal(t, 42, called)
	require.False(t, target.handler.WantsAck())
	_, err = target.handler.Invoke(context.Background(), target, event.ValueOf("wrong"))
	require.ErrorIs(t, err, ErrInvalidPayload)
}

// Covers T2
func TestTypedEventSendValidation(t *testing.T) {
	e := NewEvent[int]("value")
	target := &typedTestEndpoint{}
	require.NoError(t, e.Emit(nil, target, 42))
	require.Equal(t, "value", target.name)
	require.Equal(t, 42, target.payload.Interface())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, e.Emit(ctx, target, 0), context.Canceled)
	require.ErrorIs(t, e.Emit(nil, legacyTestEndpoint{}, 0), ErrUnsupportedEndpoint)
	require.ErrorIs(t, e.Emit(nil, nil, 0), ErrUnsupportedEndpoint)
	var nilTarget *typedTestEndpoint
	require.ErrorIs(t, e.Emit(nil, nilTarget, 0), ErrUnsupportedEndpoint)
	require.ErrorIs(t, e.HandleClient(nilTarget, func(context.Context, Endpoint, int) error { return nil }), ErrUnsupportedEndpoint)
	require.ErrorIs(t, e.HandleClient(target, nil), ErrInvalidPayload)
	require.ErrorIs(t, e.Handle(nil, nil), ErrUnsupportedEndpoint)
	for _, name := range []string{"", "connect", "disconnect", "disconnecting", "error"} {
		invalid := NewEvent[int](name)
		require.ErrorIs(t, invalid.Emit(nil, target, 0), ErrInvalidEventName)
		require.ErrorIs(t, invalid.EmitTo(nil, nil, 0), ErrInvalidEventName)
		require.ErrorIs(t, invalid.Handle(nil, nil), ErrInvalidEventName)
		require.ErrorIs(t, invalid.HandleClient(nil, nil), ErrInvalidEventName)
	}
}

// Covers T3
func TestTypedAckDescriptor(t *testing.T) {
	e := NewAckEvent[int, string]("lookup")
	require.Equal(t, "lookup", e.Name())
	target := &typedTestEndpoint{}
	require.NoError(t, e.HandleClient(target, func(_ context.Context, _ Endpoint, v int) (string, error) {
		require.Equal(t, 42, v)
		return "answer", nil
	}))
	require.True(t, target.handler.WantsAck())
	p, err := target.handler.Invoke(context.Background(), target, event.ValueOf(42))
	require.NoError(t, err)
	require.Equal(t, "answer", p.Interface())
	target.request = func(ctx context.Context, name string, p event.Payload, _ event.Schema) (event.Payload, error) {
		require.Equal(t, "lookup", name)
		require.Equal(t, 42, p.Interface())
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.InDelta(t, 30, time.Until(deadline).Seconds(), 1)
		return event.ValueOf("answer"), nil
	}
	got, err := e.EmitWithAck(nil, target, 42)
	require.NoError(t, err)
	require.Equal(t, "answer", got)
	remote := &RemoteError{Code: "denied", Message: "no"}
	target.request = func(context.Context, string, event.Payload, event.Schema) (event.Payload, error) {
		return event.Payload{}, remote
	}
	_, err = e.EmitWithAck(nil, target, 42)
	require.ErrorIs(t, err, remote)
	_, err = target.handler.Invoke(context.Background(), target, event.ValueOf("bad"))
	require.ErrorIs(t, err, ErrInvalidPayload)
}

func TestTypedAckWaitBounds(t *testing.T) {
	e := NewAckEvent[int, int]("wait")
	target := &typedTestEndpoint{request: func(ctx context.Context, _ string, _ event.Payload, _ event.Schema) (event.Payload, error) {
		<-ctx.Done()
		return event.Payload{}, ctx.Err()
	}}
	_, err := e.emitWithAck(context.Background(), target, 0, time.Millisecond)
	require.ErrorIs(t, err, ErrAckTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err = e.EmitWithAck(ctx, target, 0)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	_, err = e.EmitWithAck(ctx, target, 0)
	require.ErrorIs(t, err, context.Canceled)
}

func TestTypedAckValidation(t *testing.T) {
	e := NewAckEvent[int, int]("value")
	for _, target := range []Endpoint{nil, (*typedTestEndpoint)(nil), legacyTestEndpoint{}} {
		_, err := e.EmitWithAck(nil, target, 0)
		require.ErrorIs(t, err, ErrUnsupportedEndpoint)
	}
	require.ErrorIs(t, e.HandleClient(nil, nil), ErrUnsupportedEndpoint)
	require.ErrorIs(t, e.HandleClient(&typedTestEndpoint{}, nil), ErrInvalidPayload)
	require.ErrorIs(t, e.Handle(nil, nil), ErrUnsupportedEndpoint)
	invalid := NewAckEvent[int, int]("")
	require.ErrorIs(t, invalid.Handle(nil, nil), ErrInvalidEventName)
	require.ErrorIs(t, invalid.HandleClient(nil, nil), ErrInvalidEventName)
	_, err := invalid.EmitWithAck(nil, nil, 0)
	require.ErrorIs(t, err, ErrInvalidEventName)
	require.False(t, errors.Is(ErrInvalidPayload, ErrInvalidAck))
}
