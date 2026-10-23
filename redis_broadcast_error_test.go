package socketio

import (
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

func TestRedisBroadcastCallbackLogsWarn(t *testing.T) {
	s := miniredis.RunT(t)
	rec, _ := captureLogs(t, nil)
	p := start(t, 'S', hooks{setup: func(srv *Server) { useRedis(t, srv, s.Addr()) }})
	p.nc.Join("r")
	require.True(t, p.srv.BroadcastToRoom("/", "r", "e", "x", func() {}))
	select {
	case frame := <-p.fc.out:
		t.Fatalf("rejected callback broadcast delivered %q", frame)
	case <-time.After(200 * time.Millisecond):
	}
	logs := only(rec.since(0), "socketio: redis broadcast failed")
	require.Len(t, logs, 1)
	require.Equal(t, slog.LevelWarn.String(), logs[0]["level"])
	require.Equal(t, "/", logs[0]["nsp"])
	require.Contains(t, logs[0]["err"], "acknowledgements are unsupported")
}

func TestRedisBroadcastPublishFailureLogsWarn(t *testing.T) {
	s := miniredis.RunT(t)
	rec, _ := captureLogs(t, nil)
	p := start(t, 'S', hooks{setup: func(srv *Server) { useRedis(t, srv, s.Addr()) }})
	s.Close()
	require.True(t, p.srv.BroadcastToNamespace("/", "e", "x"))
	require.Equal(t, "2[\"e\",\"x\"]\n", recv(t, p.fc.out, "local delivery despite publication failure"))
	logs := only(rec.since(0), "socketio: redis broadcast failed")
	require.Len(t, logs, 1)
	require.Equal(t, slog.LevelWarn.String(), logs[0]["level"])
	require.Equal(t, "/", logs[0]["nsp"])
	require.NotEmpty(t, logs[0]["err"])
}
