package socketio

import (
	"testing"

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
