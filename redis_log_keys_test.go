package socketio

import (
	"testing"

	"github.com/sshaplygin/go-socket.io/logger"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

func TestRedisClusterFailureLogKeys(t *testing.T) {
	s := miniredis.RunT(t)
	rec, _ := captureLogs(t, nil)
	p := start(t, 'S', hooks{setup: func(srv *Server) { useRedis(t, srv, s.Addr()) }})
	p.nc.Join("r")
	s.Close()
	require.True(t, p.srv.ClearRoom("/", "r"))
	logs := only(rec.since(0), "socketio: redis cluster request failed")
	require.Len(t, logs, 1)
	require.Equal(t, "/", logs[0]["nsp"])
	require.NotContains(t, logs[0], "namespace")
	require.NotEmpty(t, logs[0]["err"])
}

// TestNoBadKeyAttrsRedis checks the shared logging contract on a failed Redis request.
func TestNoBadKeyAttrsRedis(t *testing.T) {
	checker := newKeyChecker()
	setDefault(t, checker)
	prev := logger.Level.Level()
	logger.Level.Set(logger.LevelTrace)
	t.Cleanup(func() { logger.Level.Set(prev) })
	s := miniredis.RunT(t)
	p := start(t, 'S', hooks{setup: func(srv *Server) { useRedis(t, srv, s.Addr()) }})
	p.nc.Join("r")
	s.Close()
	require.True(t, p.srv.ClearRoom("/", "r"))
	problems, records := checker.result("socketio: redis cluster request failed")
	require.Len(t, records, 1, "the failure must emit a checked log record")
	require.Empty(t, problems, "Redis records must follow the shared logging contract")
}
