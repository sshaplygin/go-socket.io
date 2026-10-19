package connection

import (
	"context"
	"errors"
	eventImpl "github.com/sshaplygin/go-socket.io/internal/event"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func typedRegister(t *testing.T, p *peer, name string, ack bool, invoke func(context.Context, eventImpl.Endpoint, eventImpl.Payload) (eventImpl.Payload, error)) {
	t.Helper()
	h, _ := p.handlers.Get("")
	require.NoError(t, h.RegisterEvent(name, eventImpl.NewHandler(eventImpl.SchemaOf[int64](), ack, invoke)))
}

// Covers T1
// Covers T3
func TestTypedDispatchPreservesIntegerAndErrorFirstAck(t *testing.T) {
	p := start(t, 'S', hooks{})
	seen := make(chan int64, 4)
	typedRegister(t, p, "number", true, func(ctx context.Context, _ eventImpl.Endpoint, v eventImpl.Payload) (eventImpl.Payload, error) {
		require.NoError(t, ctx.Err())
		n, err := eventImpl.Value[int64](v)
		require.NoError(t, err)
		seen <- n
		return eventImpl.ValueOf(n), nil
	})
	p.send(t, `27["number",9223372036854775807]`)
	require.Equal(t, "37[null,9223372036854775807]\n", recv(t, p.fc.out, "typed ACK"))
	require.Equal(t, int64(9223372036854775807), recv(t, seen, "typed int64"))
	p.send(t, `28["number",1,2]`)
	require.Contains(t, recv(t, p.fc.out, "invalid payload ACK"), `"code":"invalid_payload"`)
	require.ErrorIs(t, recv(t, p.errs, "invalid payload report").err, eventImpl.ErrInvalidPayload)
	p.send(t, `29["number",2]`)
	require.Equal(t, "39[null,2]\n", recv(t, p.fc.out, "connection still alive"))
	recv(t, seen, "next typed value")
	typedRegister(t, p, "failure", true, func(context.Context, eventImpl.Endpoint, eventImpl.Payload) (eventImpl.Payload, error) {
		return eventImpl.Payload{}, &eventImpl.RemoteError{Code: "denied", Message: "no"}
	})
	p.send(t, `23["failure",1]`)
	require.Equal(t, "33[{\"code\":\"denied\",\"message\":\"no\"}]\n", recv(t, p.fc.out, "error ACK"))
	recv(t, p.errs, "application error")
	typedRegister(t, p, "panic", true, func(context.Context, eventImpl.Endpoint, eventImpl.Payload) (eventImpl.Payload, error) { panic("boom") })
	p.send(t, `24["panic",1]`)
	require.Contains(t, recv(t, p.fc.out, "panic ACK"), `"handler_panic"`)
	recv(t, p.errs, "panic report")
}

// Covers T1
func TestTypedOrdinaryErrorReportsWithoutAckOrClose(t *testing.T) {
	p := start(t, 'S', hooks{events: map[string]interface{}{"barrier": func(c Conn) { c.Emit("barrier") }}})
	typedRegister(t, p, "event", false, func(context.Context, eventImpl.Endpoint, eventImpl.Payload) (eventImpl.Payload, error) {
		return eventImpl.Payload{}, errors.New("application error")
	})
	p.send(t, `21["event",1]`)
	require.ErrorContains(t, recv(t, p.errs, "typed application error").err, "application error")
	p.send(t, `2["barrier"]`)
	require.Equal(t, "2[\"barrier\"]\n", recv(t, p.fc.out, "no ordinary return ACK"))
}

// Covers T2
func TestTypedCheckedAdmissionAndLifetime(t *testing.T) {
	p := start(t, 'S', hooks{})
	nc := p.nc.(*namespaceConn)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, nc.SendEvent(ctx, "canceled", eventImpl.ValueOf(1)), context.Canceled)
	require.Error(t, nc.SendEvent(nil, "bad", eventImpl.ValueOf(make(chan int))))
	require.ErrorIs(t, nc.SendEvent(nil, "disconnect", eventImpl.ValueOf(1)), eventImpl.ErrInvalidEventName)
	require.ErrorIs(t, nc.SendEvent(nil, "invalid", eventImpl.Payload{}), eventImpl.ErrInvalidPayload)
	require.NoError(t, nc.SendEvent(nil, "number", eventImpl.ValueOf(uint64(18446744073709551615))))
	require.Equal(t, "2[\"number\",18446744073709551615]\n", recv(t, p.fc.out, "typed send"))
	nc.Disconnect()
	require.ErrorIs(t, nc.lifetime.Err(), context.Canceled)
	require.ErrorIs(t, nc.SendEvent(nil, "closed", eventImpl.ValueOf(1)), eventImpl.ErrSocketClosed)
}

// Covers T3
func TestTypedRequestAckCompletionCancellationAndDisconnect(t *testing.T) {
	for _, mode := range []string{"success", "remote error", "malformed", "canceled", "deadline", "timeout", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			p := start(t, 'S', hooks{})
			nc := p.nc.(*namespaceConn)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 20*time.Millisecond)
				defer stop()
			}
			if mode == "timeout" {
				nc.ackTimeout = 20 * time.Millisecond
			}
			done := make(chan typedResult, 1)
			go func() {
				v, err := nc.RequestAck(ctx, "question", eventImpl.ValueOf(1), eventImpl.SchemaOf[int64]())
				done <- typedResult{v, err}
			}()
			packet := recv(t, p.fc.out, "typed request")
			require.Equal(t, "21[\"question\",1]\n", packet)
			switch mode {
			case "success":
				p.send(t, `31[null,9223372036854775807]`)
			case "remote error":
				p.send(t, `31[{"code":"denied","message":"no"}]`)
			case "malformed":
				p.send(t, `31[null,"wrong"]`)
			case "canceled":
				cancel()
			case "disconnect":
				nc.Disconnect()
			}
			result := recv(t, done, "typed completion")
			switch mode {
			case "success":
				n, err := eventImpl.Value[int64](result.payload)
				require.NoError(t, err)
				require.Equal(t, int64(9223372036854775807), n)
				require.NoError(t, result.err)
			case "remote error":
				var remote *eventImpl.RemoteError
				require.ErrorAs(t, result.err, &remote)
				require.Equal(t, "denied", remote.Code)
			case "malformed":
				require.ErrorIs(t, result.err, eventImpl.ErrInvalidAck)
			case "canceled":
				require.ErrorIs(t, result.err, context.Canceled)
			case "deadline":
				require.ErrorIs(t, result.err, context.DeadlineExceeded)
			case "timeout":
				require.ErrorIs(t, result.err, eventImpl.ErrAckTimeout)
			case "disconnect":
				require.ErrorIs(t, result.err, eventImpl.ErrSocketClosed)
			}
			require.Zero(t, ackEntries(nc))
			if mode != "disconnect" {
				p.send(t, `31[null,99]`)
				require.False(t, isDone(p.conn().closing))
			}
		})
	}
}

type reentrantTypedValue struct{ send func() }

func (v reentrantTypedValue) MarshalJSON() ([]byte, error) {
	v.send()
	return []byte(`"prepared"`), nil
}

// Covers T2
func TestTypedPreparationRunsOutsideQueueLocks(t *testing.T) {
	p := start(t, 'S', hooks{})
	nc := p.nc.(*namespaceConn)
	value := reentrantTypedValue{send: func() { nc.Emit("reentrant") }}
	require.NoError(t, nc.SendEvent(nil, "outer", eventImpl.ValueOf(value)))
	require.Contains(t, recv(t, p.fc.out, "reentrant emit"), "reentrant")
	require.Contains(t, recv(t, p.fc.out, "prepared emit"), "prepared")
}

// Covers T2
// Covers T3
func TestTypedAdmissionFailureRetainsWinnerAndNoAckEntry(t *testing.T) {
	p := start(t, 'S', hooks{})
	nc := p.nc.(*namespaceConn)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, ackContextError(ctx, ctx, eventImpl.ErrSocketClosed), eventImpl.ErrSocketClosed)
	remote := &eventImpl.RemoteError{Code: "won", Message: "reply"}
	require.Same(t, remote, ackContextError(ctx, ctx, remote))
	_, err := nc.RequestAck(nil, "bad", eventImpl.ValueOf(make(chan int)), eventImpl.SchemaOf[int]())
	require.Error(t, err)
	require.Zero(t, ackEntries(nc))
	require.Zero(t, nc.id.Load())
	p.fc.hold.Store(true)
	nc.Emit("inflight")
	recv(t, p.fc.held, "blocked writer")
	for i := 0; i < defaultWriteBufferSize; i++ {
		nc.Emit("queued")
	}
	_, err = nc.RequestAck(nil, "full", eventImpl.ValueOf(1), eventImpl.SchemaOf[int]())
	require.ErrorIs(t, err, ErrWriteBufferFull)
	require.Zero(t, ackEntries(nc))
	require.Zero(t, nc.id.Load())
}

// Covers T1
// Covers T3
func TestTypedLifetimeEndsBeforeNamespaceDisconnectCallback(t *testing.T) {
	var observed context.Context
	callback := make(chan error, 1)
	p := start(t, 'S', hooks{disconnect: func(Conn) { callback <- observed.Err() }})
	observed = p.nc.(*namespaceConn).lifetime
	p.nc.Disconnect()
	require.ErrorIs(t, recv(t, callback, "canceled before callback"), context.Canceled)
}

// Covers T3
func TestTypedAckCancellationCompletionRaceRemovesEveryPendingEntry(t *testing.T) {
	for i := 0; i < 25; i++ {
		p := start(t, 'S', hooks{})
		nc := p.nc.(*namespaceConn)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan typedResult, 1)
		go func() {
			value, err := nc.RequestAck(ctx, "race", eventImpl.ValueOf(1), eventImpl.SchemaOf[int]())
			done <- typedResult{value, err}
		}()
		require.Equal(t, "21[\"race\",1]\n", recv(t, p.fc.out, "request"))
		gate := make(chan struct{})
		sent := make(chan struct{})
		go func() { <-gate; cancel() }()
		go func() { <-gate; p.fc.reads <- `31[null,2]`; close(sent) }()
		close(gate)
		result := recv(t, done, "race completion")
		recv(t, sent, "ACK consumed")
		if result.err != nil {
			require.ErrorIs(t, result.err, context.Canceled)
		} else {
			value, err := eventImpl.Value[int](result.payload)
			require.NoError(t, err)
			require.Equal(t, 2, value)
		}
		require.Zero(t, ackEntries(nc))
		cancel()
	}
}
