package namespace

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// Covers G18
func TestRegressionG18ConnectPanicIsContained(t *testing.T) {
	h := NewHandler()
	h.OnConnect(func(Conn) error { panic("application panic") })
	require.NotPanics(t, func() { require.Error(t, h.Connect(nil)) })
}

func TestConnectMiddlewarePanicAndNil(t *testing.T) {
	h := NewHandler()
	h.Use(nil)
	require.NoError(t, h.Connect(nil))
	h.Use(func(Conn) error { panic("middleware panic") })
	require.ErrorContains(t, h.Connect(nil), "middleware panic")
}
