package client

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Covers K4 (C)
func TestConnEngineConn(t *testing.T) { p := start(t, 'C', hooks{}); require.Nil(t, p.nc.EngineConn()) }

// Covers K5 (C)
func TestConnRequest(t *testing.T) {
	p := start(t, 'C', hooks{})
	require.Nil(t, p.nc.Request())
	require.Equal(t, Handshake{}, p.nc.Handshake())
}
