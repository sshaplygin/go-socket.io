package redis

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redigo "github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/require"
)

// The network poller and context timer run independently. This context models
// the interval after the deadline passes but before the context timer runs.
type pendingDeadlineContext struct {
	context.Context
	deadline time.Time
}

func (c pendingDeadlineContext) Deadline() (time.Time, bool) { return c.deadline, true }

func TestRedisRequestNormalizesDialDeadlineBeforeContextTimer(t *testing.T) {
	s := miniredis.RunT(t)
	a := newTestRedisBroadcast(t, s)
	networkTimeout := &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}
	dial := func(context.Context) (redigo.Conn, error) { return nil, networkTimeout }
	a.transport().dial.Store(&dial)
	ctx := pendingDeadlineContext{Context: context.Background(), deadline: time.Now().Add(-time.Second)}
	require.NoError(t, ctx.Err(), "the context timer has not delivered cancellation")
	require.ErrorIs(t, a.RemoteJoin(ctx, "socket", "room"), context.DeadlineExceeded)
	// An earlier independent transport timeout must retain its transport cause.
	ctx.deadline = time.Now().Add(time.Hour)
	require.ErrorIs(t, a.RemoteJoin(ctx, "socket", "room"), networkTimeout)
}

func TestRedisRequestNormalizesInternalDeadlineBeforeContextTimer(t *testing.T) {
	caller := context.Background()
	wait := pendingDeadlineContext{Context: caller, deadline: time.Now().Add(-time.Second)}
	require.ErrorIs(t, requestContextError(caller, wait, os.ErrDeadlineExceeded), ErrRequestTimeout)
}
