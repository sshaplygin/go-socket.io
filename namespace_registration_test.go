package socketio

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandlerRegistrationIdentity(t *testing.T) {
	h := newNamespaceHandler()
	f := func(Conn) error { return nil }
	h.OnConnect(f)
	require.Equal(t, reflect.ValueOf(f).Pointer(), reflect.ValueOf(h.onConnect).Pointer())
	g := func(Conn) error { return errors.New("second") }
	h.OnConnect(g)
	require.Equal(t, reflect.ValueOf(g).Pointer(), reflect.ValueOf(h.onConnect).Pointer())
	h.OnEvent("event", func(Conn, int) {})
	require.Equal(t, []reflect.Type{reflect.TypeOf(0)}, h.getEventTypes("event"))
	disconnect := func(Conn, string) {}
	onError := func(Conn, error) {}
	h.OnDisconnect(disconnect)
	h.OnError(onError)
	require.Equal(t, reflect.ValueOf(disconnect).Pointer(), reflect.ValueOf(h.onDisconnect).Pointer())
	require.Equal(t, reflect.ValueOf(onError).Pointer(), reflect.ValueOf(h.onError).Pointer())
}

func TestParallelEventRegistration(t *testing.T) {
	h := newNamespaceHandler()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; h.OnEvent(fmt.Sprintf("e%d", i), func(Conn) {}) }()
	}
	close(start)
	wg.Wait()
	require.Len(t, h.events, 8)
}
