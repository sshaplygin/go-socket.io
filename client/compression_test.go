package client

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

type compressionClientConn struct {
	*fakeConn
	flags chan bool
}

func (c *compressionClientConn) NextWriterCompress(ft eioparser.FrameType, compress bool) (io.WriteCloser, error) {
	c.flags <- compress
	return c.fakeConn.NextWriter(ft)
}

// Covers C11
func TestClientCompressBuffered(t *testing.T) {
	cl, err := New("http://unused/", WithReconnection(!(true)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	fc := newFakeConn(t)
	wire := &compressionClientConn{fakeConn: fc, flags: make(chan bool, 16)}
	cl.dial = func(string) (engineclient.Conn, error) { return wire, nil }
	ready := make(chan Conn, 1)
	cl.OnConnect(func(c Conn) error { ready <- c; return nil })
	require.NoError(t, cl.Connect())
	closeAtEnd(t, fc, cl.conn)
	nc, ok := cl.conn.namespaces.Get(rootNamespace)
	require.True(t, ok)
	base := nc.Compress(false)
	require.NoError(t, base.Emit("buffered plain"))
	require.NoError(t, base.Compress(true).Emit("buffered compressed"))
	require.NoError(t, base.Volatile().Emit("dropped before CONNECT"))
	require.ErrorIs(t, base.To("room").Emit("server only"), ErrServerOnly)
	fc.reads <- "0"
	recv(t, ready, "namespace CONNECT")
	for _, tc := range []struct {
		event string
		flag  bool
	}{{"buffered plain", false}, {"buffered compressed", true}} {
		require.Equal(t, ev(tc.event), recv(t, fc.out, "buffered event"))
		require.Equal(t, tc.flag, recv(t, wire.flags, "compression flag"))
	}
	require.NoError(t, base.Volatile().Local().Send("after CONNECT"))
	require.Equal(t, ev("message", "after CONNECT"), recv(t, fc.out, "volatile event"))
	require.False(t, recv(t, wire.flags, "volatile compression flag"))
	nc.Emit("default")
	require.Equal(t, ev("default"), recv(t, fc.out, "default event"))
	require.True(t, recv(t, wire.flags, "default compression flag"))
}
