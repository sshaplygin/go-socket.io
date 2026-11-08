package socketio

import (
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/parser"
)

func TestServerCloseAfterEmptyQueueObservation(t *testing.T) {
	for _, packets := range []string{"application", "disconnect", "application and disconnect"} {
		t.Run(packets, func(t *testing.T) {
			engine := newFakeConn(t)
			c := newConnection(engine, newNamespaceRegistry(), 1, time.Minute, slog.Default())
			t.Cleanup(func() { c.stop(); recv(t, engine.closed, "engine cleanup") })

			// Reproduce the writer's stale empty observation without depending on
			// the scheduler: packets arrive and Close seals before its decision.
			_, queued := c.nextQueued()
			require.False(t, queued)
			require.False(t, c.writeDrained(), "an open empty queue is not drained")
			var want []string
			if packets != "disconnect" {
				c.write(parser.Header{Type: parser.Event}, reflect.ValueOf("last"))
				want = append(want, ev("last"))
			}
			if packets != "application" {
				c.write(parser.Header{Type: parser.Disconnect})
				want = append(want, "1")
			}
			require.NoError(t, c.Close())
			require.True(t, isDone(c.seal))
			require.False(t, c.writeDrained(), "Close must not reuse the stale empty observation")

			c.serveWrite()
			recv(t, engine.closed, "engine closes after admitted packets")
			for _, packet := range want {
				require.Equal(t, packet, recv(t, engine.out, "admitted packet"))
			}
			require.Empty(t, engine.out)
			require.True(t, c.writeDrained())
		})
	}
}
