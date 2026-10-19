package connection

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/internal/event"
	"github.com/stretchr/testify/require"
)

// Covers T1, T2, T3.
func TestTypedClientDispatchAndRegistration(t *testing.T) {
	p := start(t, 'C', hooks{})
	s := p.cl.Socket("/")
	calls := make(chan int, 4)
	h := event.NewHandler(event.SchemaOf[int](), true, func(ctx context.Context, to event.Endpoint, v event.Payload) (event.Payload, error) {
		n, err := event.Value[int](v)
		if err != nil {
			return event.Payload{}, err
		}
		calls <- n
		return event.ValueOf(n + 1), nil
	})
	require.NoError(t, s.RegisterEvent("typed", h))
	require.ErrorIs(t, s.RegisterEvent("typed", h), event.ErrDuplicateEvent)
	s.OnEvent("legacy", func(Conn, int) {})
	require.ErrorIs(t, s.RegisterEvent("legacy", h), event.ErrDuplicateEvent)
	p.send(t, `27["typed",41]`)
	require.Equal(t, 41, recv(t, calls, "typed invocation"))
	require.Equal(t, `37[null,42]`, strings.TrimSpace(recv(t, p.fc.out, "typed acknowledgement")))
	p.send(t, `28["typed","bad"]`)
	require.Contains(t, recv(t, p.fc.out, "invalid payload acknowledgement"), `"code":"invalid_payload"`)
	require.ErrorIs(t, recv(t, p.errs, "decode error").err, event.ErrInvalidPayload)
	p.send(t, `29["typed",1,2]`)
	require.Contains(t, recv(t, p.fc.out, "arity failure acknowledgement"), `"code":"invalid_payload"`)
	recv(t, p.errs, "arity error")
	s.Off("typed")
	require.NoError(t, s.RegisterEvent("typed", h))
	s.OnEvent("typed", func(Conn, int) { calls <- 99 })
	p.send(t, `2["typed",0]`)
	require.Equal(t, 99, recv(t, calls, "legacy replacement"))
}

// Covers T2, T3.
func TestTypedClientRequestAckAndCancellation(t *testing.T) {
	p := start(t, 'C', hooks{})
	endpoint := p.nc.(event.TypedEndpoint)
	request := func(ctx context.Context) <-chan typedResult {
		done := make(chan typedResult, 1)
		go func() {
			v, e := endpoint.RequestAck(ctx, "ask", event.ValueOf(7), event.SchemaOf[int]())
			done <- typedResult{v, e}
		}()
		return done
	}
	done := request(context.Background())
	frame := strings.TrimSpace(recv(t, p.fc.out, "outgoing typed request"))
	id := strings.TrimSuffix(strings.TrimPrefix(frame, "2"), `["ask",7]`)
	p.send(t, "3"+id+`[null,9007199254740993]`)
	result := recv(t, done, "exact integer ACK")
	require.NoError(t, result.err)
	n, err := event.Value[int](result.value)
	require.NoError(t, err)
	require.EqualValues(t, 9007199254740993, n)
	ctx, cancel := context.WithCancel(context.Background())
	done = request(ctx)
	frame = strings.TrimSpace(recv(t, p.fc.out, "cancelled request"))
	id = strings.TrimSuffix(strings.TrimPrefix(frame, "2"), `["ask",7]`)
	cancel()
	require.ErrorIs(t, recv(t, done, "cancellation").err, context.Canceled)
	p.conn().mu.Lock()
	require.Empty(t, p.nc.(*namespaceConn).typedAcks)
	p.conn().mu.Unlock()
	p.send(t, "3"+id+`[null,9]`)
	done = request(context.Background())
	frame = strings.TrimSpace(recv(t, p.fc.out, "post cancellation request"))
	id = strings.TrimSuffix(strings.TrimPrefix(frame, "2"), `["ask",7]`)
	p.send(t, "3"+id+`[{"code":"denied","message":"no"}]`)
	var remote *event.RemoteError
	require.ErrorAs(t, recv(t, done, "remote error").err, &remote)
	require.Equal(t, "denied", remote.Code)
	done = request(context.Background())
	recv(t, p.fc.out, "disconnect request")
	require.NoError(t, p.Close())
	require.ErrorIs(t, recv(t, done, "disconnect completion").err, event.ErrSocketClosed)
	require.ErrorIs(t, endpoint.SendEvent(context.Background(), "ask", event.ValueOf(1)), event.ErrSocketClosed)
}

// Covers T2, T3.
func TestTypedClientPreconnectCancellationAndClose(t *testing.T) {
	p := newPeer(t, 'C', hooks{})
	s := p.cl.Socket("/")
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, s.SendEvent(ctx, "expired", event.ValueOf(1)))
	cancel()
	// A request awaiting cancellation synchronously removes its own buffer entry.
	reqctx, reqcancel := context.WithCancel(context.Background())
	reqcancel()
	_, err := s.RequestAck(reqctx, "ask", event.ValueOf(1), event.SchemaOf[int]())
	require.ErrorIs(t, err, context.Canceled)
	p.connect(t)
	require.NoError(t, s.SendEvent(context.Background(), "barrier", event.ValueOf(2)))
	require.Equal(t, `2["barrier",2]`, strings.TrimSpace(recv(t, p.fc.out, "only live packet flushed")))
	// Cancellation is checked at admission even if its asynchronous cleanup has not run.
	require.ErrorIs(t, s.SendEvent(ctx, "expired", event.ValueOf(1)), context.Canceled)
}

// Covers T1, T3.
func TestTypedClientHandlerFailureAcknowledgesAndReports(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "panic"}[panics], func(t *testing.T) {
			p := start(t, 'C', hooks{})
			require.NoError(t, p.cl.RegisterEvent("fail", event.NewHandler(event.SchemaOf[int](), true, func(context.Context, event.Endpoint, event.Payload) (event.Payload, error) {
				if panics {
					panic("failure")
				}
				return event.Payload{}, errors.New("failure")
			})))
			p.send(t, `23["fail",1]`)
			ack := recv(t, p.fc.out, "error acknowledgement")
			if panics {
				require.Contains(t, ack, `"code":"handler_panic"`)
			} else {
				require.Contains(t, ack, `"code":"handler_error"`)
			}
			require.Error(t, recv(t, p.errs, "local handler error").err)
		})
	}
}

// Covers T2, T3.
func TestTypedClientLifetimeEndsBeforeDisconnect(t *testing.T) {
	entered := make(chan context.Context, 1)
	released := make(chan struct{})
	callback := make(chan error, 1)
	p := start(t, 'C', hooks{disconnect: func(c Conn) { callback <- c.(*namespaceConn).handlerContext().Err() }})
	require.NoError(t, p.cl.RegisterEvent("hold", event.NewHandler(event.SchemaOf[int](), false, func(ctx context.Context, _ event.Endpoint, _ event.Payload) (event.Payload, error) {
		entered <- ctx
		<-ctx.Done()
		close(released)
		return event.Payload{}, nil
	})))
	p.nc.SetContext("application context is independent")
	p.send(t, `2["hold",1]`)
	ctx := recv(t, entered, "handler entry")
	require.NoError(t, p.Close())
	recv(t, released, "handler cancellation")
	require.ErrorIs(t, recv(t, callback, "disconnect callback context"), context.Canceled)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

// Covers T2, T3.
func TestTypedClientPreconnectRequestBudgetRelease(t *testing.T) {
	p := newPeer(t, 'C', hooks{})
	s := p.cl.Socket("/")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.RequestAck(ctx, "cancelled", event.ValueOf(1), event.SchemaOf[int]()); done <- err }()
	require.Eventually(t, func() bool { p.cl.state.mu.Lock(); defer p.cl.state.mu.Unlock(); return p.cl.state.buffered == 1 }, waitFor, time.Millisecond)
	cancel()
	require.ErrorIs(t, recv(t, done, "preconnect request cancellation"), context.Canceled)
	p.cl.state.mu.Lock()
	require.Zero(t, p.cl.state.buffered)
	require.Empty(t, s.buffer)
	p.cl.state.mu.Unlock()
	for i := 0; i < defaultWriteBufferSize; i++ {
		require.NoError(t, s.SendEvent(context.Background(), "queued", event.ValueOf(i)))
	}
	require.ErrorIs(t, s.SendEvent(context.Background(), "overflow", event.ValueOf(1)), ErrWriteBufferFull)
	go func() {
		_, err := s.RequestAck(context.Background(), "overflow", event.ValueOf(1), event.SchemaOf[int]())
		done <- err
	}()
	require.ErrorIs(t, recv(t, done, "preconnect overflow"), ErrWriteBufferFull)
	require.NoError(t, s.Close())
	p.cl.state.mu.Lock()
	require.Zero(t, p.cl.state.buffered)
	require.Empty(t, s.buffer)
	p.cl.state.mu.Unlock()
}

// Covers T2, T3.
func TestTypedClientPreconnectCloseCompletesRequest(t *testing.T) {
	p := newPeer(t, 'C', hooks{})
	done := make(chan error, 1)
	go func() {
		_, err := p.cl.RequestAck(context.Background(), "pending", event.ValueOf(1), event.SchemaOf[int]())
		done <- err
	}()
	require.Eventually(t, func() bool { p.cl.state.mu.Lock(); defer p.cl.state.mu.Unlock(); return p.cl.state.buffered == 1 }, waitFor, time.Millisecond)
	require.NoError(t, p.cl.Socket("/").Close())
	require.ErrorIs(t, recv(t, done, "preconnect close"), event.ErrSocketClosed)
}

// Covers T2, T3.
func TestTypedClientMalformedAckRemainsUsable(t *testing.T) {
	p := start(t, 'C', hooks{})
	for _, body := range []string{`[42]`, `[null]`, `[null,1,2]`, `[{"message":"missing code"}]`, `[null,"wrong"]`} {
		done := make(chan error, 1)
		go func() {
			_, err := p.cl.RequestAck(context.Background(), "ask", event.ValueOf(7), event.SchemaOf[int]())
			done <- err
		}()
		frame := strings.TrimSpace(recv(t, p.fc.out, "request"))
		id := strings.TrimSuffix(strings.TrimPrefix(frame, "2"), `["ask",7]`)
		p.send(t, "3"+id+body)
		require.ErrorIs(t, recv(t, done, "invalid acknowledgement"), event.ErrInvalidAck)
	}
	require.NoError(t, p.cl.SendEvent(context.Background(), "still alive", event.ValueOf(1)))
	require.Equal(t, ev("still alive", 1), recv(t, p.fc.out, "healthy transport"))
}
