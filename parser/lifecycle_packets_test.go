package parser

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrorPacket(t *testing.T) {
	for _, value := range []interface{}{"Invalid namespace", map[string]interface{}{"message": "denied"}, []interface{}{"detail"}, nil} {
		w := &fakeWriter{}
		require.NoError(t, NewEncoder(w).Encode(Header{Type: Error, Namespace: "/chat"}, value))
		d, _ := validationDecoder(w.data[0].String())
		var h Header
		var event string
		require.NoError(t, d.DecodeHeader(&h, &event))
		require.Equal(t, Error, h.Type)
		require.Equal(t, "/chat", h.Namespace)
		args, err := d.DecodeArgs([]reflect.Type{reflect.TypeOf((*interface{})(nil)).Elem()})
		require.NoError(t, err)
		require.Equal(t, value, args[0].Interface())
	}
}

func TestDisconnectPacket(t *testing.T) {
	for namespace, want := range map[string]string{"": "1", "/chat": "1/chat,"} {
		w := &fakeWriter{}
		require.NoError(t, NewEncoder(w).Encode(Header{Type: Disconnect, Namespace: namespace}))
		require.Equal(t, want, w.data[0].String())
	}
}
