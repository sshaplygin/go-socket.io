package socketio

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

type countedBroadcastValue struct{ calls *atomic.Int32 }

func (v countedBroadcastValue) MarshalJSON() ([]byte, error) {
	v.calls.Add(1)
	return []byte(`"payload"`), nil
}

func TestBroadcastMarshalsPayloadOnce(t *testing.T) {
	srv, addr := defectServer(t, func(s *Server) {
		s.OnConnect("/", func(c Conn) error { c.Join("r"); return nil })
	})
	var peers []*rawClient
	for i := 0; i < 10; i++ {
		c := dialRaw(t, addr)
		require.Equal(t, "0", c.read())
		peers = append(peers, c)
	}
	var calls atomic.Int32
	require.True(t, srv.BroadcastToRoom("/", "r", "news", countedBroadcastValue{&calls}))
	for _, c := range peers {
		require.Equal(t, "2[\"news\",\"payload\"]\n", c.read())
	}
	require.Equal(t, int32(1), calls.Load())
}
