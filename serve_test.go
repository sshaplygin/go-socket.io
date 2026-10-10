package socketio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestServeReturnsNilAfterClose checks that Serve treats the end of the
// accept loop caused by Close as a normal shutdown.
func TestServeReturnsNilAfterClose(t *testing.T) {
	srv := NewServer(nil)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve() }()

	require.NoError(t, srv.Close())
	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after Close")
	}
}
