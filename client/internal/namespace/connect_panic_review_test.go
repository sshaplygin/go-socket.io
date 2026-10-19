package namespace

import (
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

// Covers G18
func TestClientConnectCallbackPanicReturnsError(t *testing.T) {
	want := errors.New("connect panic")
	handler := NewHandler("/")
	handler.OnConnect(func(Conn) error { panic(want) })
	var got error
	require.NotPanics(t, func() { got = handler.ConnectHandler()(nil) })
	require.ErrorIs(t, got, want)
}
