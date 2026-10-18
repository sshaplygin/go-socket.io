package socketio

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

type customCloseConn struct {
	engineio.Conn
	err    error
	closes int
	aborts int
}

func (c *customCloseConn) Close() error { c.closes++; return c.err }
func (c *customCloseConn) Abort() error { c.aborts++; return nil }

func TestAbortCustomEngineUsesClose(t *testing.T) {
	want := errors.New("custom transport close")
	c := &customCloseConn{err: want}
	require.ErrorIs(t, engineio.Abort(c), want)
	require.Equal(t, 1, c.closes)
	require.Zero(t, c.aborts)
}
