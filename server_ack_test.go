package socketio

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEmitAckCallbackIsOneShot checks that the ACK callback of a server Emit runs
// once: an ACK that repeats the id is ignored, as socket.io 2.5.0 deletes the
// callback after its first call.
func TestEmitAckCallbackIsOneShot(t *testing.T) {
	calls := make(chan string, 4)
	done := make(chan struct{}, 1)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(c Conn) error {
			c.Emit("ask", func(reply string) { calls <- reply })
			return nil
		})
		s.OnEvent("/", "done", func(Conn) { done <- struct{}{} })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	require.Equal(t, "21[\"ask\"]\n", c.read())
	c.send(`31["one"]`)
	c.send(`31["two"]`)
	// Packets are handled in order, so both ACKs have been handled once "done" is.
	c.send(`2["done"]`)
	recv(t, done, "done event")
	require.Equal(t, "one", recv(t, calls, "ACK callback"))
	select {
	case reply := <-calls:
		t.Fatalf("the ACK callback ran again with %q", reply)
	default:
	}
}

// TestUnlistenedEventWithMiddlewareGetsNoAck checks that an event without listeners
// gets no ACK when socket middleware observes it, as without middleware. With
// socket.io 2.5.0 and a socket.use middleware, `21["unknown",1]` reaches the
// middleware and no ACK is sent; the next reply is the ACK of a listened event.
func TestUnlistenedEventWithMiddlewareGetsNoAck(t *testing.T) {
	seen := make(chan string, 4)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(c Conn) error {
			c.Use(func(event string, _ []interface{}) error {
				seen <- event
				return nil
			})
			return nil
		})
		s.OnEvent("/", "alive", func(Conn) string { return "alive" })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send(`21["unknown",1]`)
	c.send(`22["alive"]`)
	require.Equal(t, "32[\"alive\"]\n", c.read(), "the event without listeners was acknowledged")
	require.Equal(t, "unknown", recv(t, seen, "middleware"))
	require.Equal(t, "alive", recv(t, seen, "middleware"))
}
