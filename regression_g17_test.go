package socketio

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// Covers G17
func TestRegressionG17RootAlwaysExists(t *testing.T) {
	ts := newTestServer(t, func(*Server) {})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
}
