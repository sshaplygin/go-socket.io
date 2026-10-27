package socketio

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
)

func TestConnEmitWithOptions(t *testing.T) {
	t.Run("compression is per emission and selection is already resolved", func(t *testing.T) {
		connected := make(chan Conn, 1)
		_, ts := newCompressedServer(t, func(c Conn) { connected <- c })
		peer := compressedConnect(t, ts)
		conn := compressionReceive(t, connected)
		payload := strings.Repeat("x", 4096)

		// Adapter selection has already resolved this socket. Its direct send
		// applies delivery flags even if the original room filters differ.
		conn.EmitWithOptions(adapter.Options{
			Rooms:      []string{"not-joined"},
			Except:     []string{conn.ID()},
			NoCompress: true,
		}, "plain", payload)
		compressed, data := peer.message(t)
		require.False(t, compressed)
		require.Equal(t, "42[\"plain\",\""+payload+"\"]\n", string(data))

		conn.EmitWithOptions(adapter.Options{}, "default", payload)
		compressed, data = peer.message(t)
		require.True(t, compressed)
		require.Equal(t, "42[\"default\",\""+payload+"\"]\n", string(data))
	})

	t.Run("volatile drops behind a busy writer without consuming an ACK ID", func(t *testing.T) {
		p := start(t, 'S', hooks{})
		p.fc.hold.Store(true)
		p.nc.Emit("blocked")
		recv(t, p.fc.held, "held writer")
		p.nc.Emit("queued")

		p.nc.EmitWithOptions(adapter.Options{Volatile: true}, "dropped", func() {})
		p.fc.hold.Store(false)
		p.fc.release <- struct{}{}
		expectBeforeBarrier(t, p, "2[\"blocked\"]\n", "2[\"queued\"]\n")
		assertNextAckID(t, p, 1)
		require.True(t, p.nc.Connected())
	})
}
