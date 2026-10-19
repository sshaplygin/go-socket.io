package namespace

import (
	"github.com/stretchr/testify/require"
	"reflect"
	"testing"
)

// Covers G20
func TestRegressionG20VariadicHandlerArguments(t *testing.T) {
	called := false
	h := NewEventFunc(func(_ Conn, args ...string) { called = true })
	_, err := h.Call([]reflect.Value{reflect.Zero(reflect.TypeOf((*Conn)(nil)).Elem()), reflect.ValueOf([]string{"a", "b"})})
	require.NoError(t, err, "registered argument type is []string, but dispatch cannot call the variadic handler with it")
	require.True(t, called)
}
