package socketio

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// A typed descriptor's handler keeps the connection on an argument decode error that
// closes it for an untyped handler: the error is reported to OnError, a requested
// acknowledgement is answered with the invalid_payload error ACK, and a later event is
// answered.
func TestTypedHandlerDecodeErrorKeepsConnection(t *testing.T) {
	s := newRawDecodeServer(t, func(srv *Server) {
		require.NoError(t, NewAckEvent[time.Time, string]("at").Handle(srv.Of("/"), func(_ context.Context, _ Conn, ts time.Time) (string, error) {
			return ts.String(), nil
		}))
		require.NoError(t, NewEvent[time.Time]("note").Handle(srv.Of("/"), func(context.Context, Conn, time.Time) error { return nil }))
	})
	s.send(t, `421["at","bad"]`)
	require.ErrorContains(t, recv(t, s.errs, "the decode error of the acknowledged event"), "decode argument 0: parsing time")
	require.Contains(t, s.next(t, "431"), `431[{"code":"invalid_payload","message":"socketio: invalid typed payload: decode argument 0: parsing time`)
	s.send(t, `42["note","bad"]`)
	require.ErrorContains(t, recv(t, s.errs, "the decode error of the plain event"), "decode argument 0: parsing time")
	s.send(t, `422["ping"]`)
	require.Equal(t, `432["pong"]`, s.next(t, "432"))
	require.Empty(t, drain(s.reasons), "the connection ended")
}

// The arguments of an ACK decode into the parameters of the Emit callback under the
// rules of an untyped handler's arguments. A wrong JSON type is reported to OnError, the
// callback is not called and the connection stays open; any other decode error is
// reported while connected and then closes the connection with forced close.
func TestAckCallbackArgumentDecodeErrors(t *testing.T) {
	s := newRawDecodeServer(t)
	c := recv(t, s.conns, "OnConnect")
	called := make(chan struct{}, 2)

	c.Emit("q", func(int) { called <- struct{}{} })
	s.send(t, "43"+s.ackRequest(t, "q")+`["x"]`)
	require.ErrorContains(t, recv(t, s.errs, "the type mismatch"), "decode argument 0: json: cannot unmarshal string")
	s.send(t, `422["ping"]`)
	require.Equal(t, `432["pong"]`, s.next(t, "432"))
	require.Empty(t, drain(s.reasons), "the connection ended")

	c.Emit("q", func(time.Time) { called <- struct{}{} })
	s.send(t, "43"+s.ackRequest(t, "q")+`["bad"]`)
	err := recv(t, s.errs, "the decode error")
	require.ErrorContains(t, err, "decode argument 0: parsing time")
	require.NotContains(t, err.Error(), "after the close")
	require.Equal(t, "forced close", recv(t, s.reasons, "OnDisconnect"))
	s.closed(t)
	require.Empty(t, drain(called), "a callback was called")
}

// The Go client decodes ACK arguments into its Emit callbacks as the server does, and
// its typed handlers keep the connection on an argument decode error.
func TestClientAckCallbackArgumentDecodeErrors(t *testing.T) {
	p := newHeartbeatPair(t, websocket.Default, 25*time.Second, 20*time.Second, func(p *heartbeatPair) {
		p.srv.OnEvent("/", "word", func(Conn) string { return "bad" })
		p.srv.OnEvent("/", "echo", func(Conn) string { return "pong" })
		require.NoError(t, NewAckEvent[time.Time, string]("at").HandleClient(p.cl, func(_ context.Context, _ Endpoint, ts time.Time) (string, error) {
			return ts.String(), nil
		}))
	})
	c := recv(t, p.conns, "OnConnect")
	called, acks, codes := make(chan struct{}, 2), make(chan string, 2), make(chan string, 1)
	alive := func(what string) {
		t.Helper()
		p.cl.Emit("echo", func(s string) { acks <- s })
		require.Equal(t, "pong", recv(t, acks, what))
	}

	c.Emit("at", "bad", func(e map[string]string) { codes <- e["code"] })
	require.Equal(t, "invalid_payload", recv(t, codes, "the error ACK of the typed client handler"))
	require.ErrorContains(t, recv(t, p.errs, "the typed handler's decode error"), "decode argument 0: parsing time")
	alive("the ACK after the typed handler's decode error")

	p.cl.Emit("word", func(int) { called <- struct{}{} })
	require.ErrorContains(t, recv(t, p.errs, "the type mismatch"), "decode argument 0: json: cannot unmarshal string")
	alive("the ACK after the type mismatch")

	p.cl.Emit("word", func(time.Time) { called <- struct{}{} })
	require.ErrorContains(t, recv(t, p.errs, "the decode error"), "decode argument 0: parsing time")
	require.Equal(t, "forced close", recv(t, p.clientReason, "the client's OnDisconnect"))
	recv(t, p.serverReasons, "the server's OnDisconnect")
	require.Empty(t, drain(called), "a callback was called")
	require.Empty(t, drain(p.errs), "OnError was called again")
}

// An Emit callback has no timeout and is not called when its connection ends before the
// ACK arrives.
func TestEmitCallbackDroppedWhenConnectionEnds(t *testing.T) {
	t.Run("server", func(t *testing.T) {
		s := newRawDecodeServer(t)
		c := recv(t, s.conns, "OnConnect")
		called := make(chan struct{}, 1)
		c.Emit("q", func(string) { called <- struct{}{} })
		s.ackRequest(t, "q")
		require.NoError(t, s.ws.Close())
		recv(t, s.reasons, "OnDisconnect")
		recv(t, serverCompletion(s.srv, c.ID()), "the end of the connection")
		require.Empty(t, drain(called), "the callback was called")
		require.Empty(t, drain(s.errs), "OnError was called")
	})
	t.Run("client", func(t *testing.T) {
		held := make(chan AckFunc, 1)
		p := newHeartbeatPair(t, websocket.Default, 25*time.Second, 20*time.Second, func(p *heartbeatPair) {
			p.srv.OnEvent("/", "hold", func(_ Conn, ack AckFunc) { held <- ack })
		})
		c := recv(t, p.conns, "OnConnect")
		called := make(chan struct{}, 1)
		p.cl.Emit("hold", func(string) { called <- struct{}{} })
		ack := recv(t, held, "the handler call")
		require.NoError(t, c.Close())
		recv(t, p.clientReason, "the client's OnDisconnect")
		recv(t, p.serverReasons, "the server's OnDisconnect")
		ack("late")
		require.Empty(t, drain(called), "the callback was called")
		require.Empty(t, drain(p.errs), "OnError was called")
	})
}
