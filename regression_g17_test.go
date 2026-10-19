package socketio

import (
 "testing"
 "github.com/stretchr/testify/require"
)

// Covers: G17
func TestRegressionG17RootAlwaysExists(t *testing.T) {
 ts := newTestServer(t, func(*Server) {})
 c := dialRaw(t,ts.URL)
 require.Equal(t,"0",c.read())
}
