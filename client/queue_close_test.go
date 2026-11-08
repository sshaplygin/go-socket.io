package client

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/parser"
)

func TestClientCloseAfterEmptyQueueObservation(t *testing.T) {
	for _, packets := range []string{"application", "disconnect", "application and disconnect"} {
		t.Run(packets, func(t *testing.T) {
			engine := newFakeConn(t)
			c := newConn(engine, newNamespaceRegistry(), connLimits{1, time.Minute}, slog.Default())
			t.Cleanup(func() { c.stop(); recv(t, engine.closed, "engine cleanup") })

			// Pause the writer logically between its empty observation and its
			// drain decision. Admission and Close run in that exact window.
			_, queued := c.nextQueued()
			require.False(t, queued)
			require.False(t, c.writeDrained(), "an open empty queue is not drained")
			var want []string
			if packets != "disconnect" {
				header := parser.Header{Type: parser.Event}
				prepared, err := parser.Prepare(header, []interface{}{"last"})
				require.NoError(t, err)
				c.mu.Lock()
				c.queuePreparedLocked(header, false, prepared, nil)
				c.mu.Unlock()
				want = append(want, ev("last"))
			}
			if packets != "application" {
				c.writeControl(parser.Header{Type: parser.Disconnect})
				want = append(want, "1")
			}
			require.NoError(t, c.Close())
			require.True(t, isDone(c.seal))
			require.False(t, c.writeDrained(), "Close must not reuse the stale empty observation")

			// The writer must revisit both queues, preserving application/control
			// ordering when DISCONNECT must follow an earlier application packet.
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
