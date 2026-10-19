package namespace

import (
	"testing"
	"github.com/stretchr/testify/require"
)

// Covers: G18
func TestRegressionG18ConnectPanicIsContained(t *testing.T) {
	h := NewHandler()
	h.OnConnect(func(Conn) error { panic("application panic") })
	require.NotPanics(t, func() { require.Error(t, h.Connect(nil)) })
}

