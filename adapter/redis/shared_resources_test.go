package redis

import (
	"fmt"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"io"
	"testing"
	"time"
)

// Covers G14
func TestFactorySharesRedisConnectionsAcrossNamespaces(t *testing.T) {
	s := miniredis.RunT(t)
	factory := New(WithAddr(s.Addr()))
	var namespaces []io.Closer
	for i := 0; i < 30; i++ {
		a, err := factory(fmt.Sprintf("/room-%d", i))
		require.NoError(t, err)
		namespaces = append(namespaces, a.(io.Closer))
	}
	t.Cleanup(func() {
		for _, n := range namespaces {
			_ = n.Close()
		}
	})
	require.LessOrEqual(t, s.CurrentConnectionCount(), 2, "namespace count must not allocate a connection pair per namespace")
	require.NoError(t, namespaces[0].Close())
	require.Equal(t, 2, s.CurrentConnectionCount(), "other namespaces still own the shared connections")
	for _, n := range namespaces {
		require.NoError(t, n.Close())
	}
	require.Eventually(t, func() bool { return s.CurrentConnectionCount() == 0 }, time.Second, time.Millisecond)
}
