//go:build js && wasm

package browsertest

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/sshaplygin/go-socket.io/parser"
)

func receive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout: %s", what)
		var zero T
		return zero
	}
}

// connect connects the namespaces over websocket only: the client dials with the
// browser's WebSocket in place of websocket.Default.
func connect(t *testing.T, path string, namespaces []string, opts ...client.Option) (*client.Client, []*client.Socket) {
	t.Helper()
	opts = append([]client.Option{client.WithPath(path), client.WithTransports(websocket.Default), client.WithReconnection(false)}, opts...)
	cl, err := client.New(os.Getenv("SOCKETIO_BROWSER_URL"), opts...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cl.Close()) })
	connected := make(chan string, len(namespaces))
	sockets := make([]*client.Socket, len(namespaces))
	for i, nsp := range namespaces {
		sockets[i] = cl.Socket(nsp)
		sockets[i].OnConnect(func(c client.Conn) error { connected <- c.Namespace(); return nil })
	}
	require.NoError(t, cl.Connect())
	var names []string
	for range namespaces {
		names = append(names, receive(t, connected, "CONNECT"))
	}
	require.ElementsMatch(t, namespaces, names)
	return cl, sockets
}

func TestNamespacesEventsAndAcks(t *testing.T) {
	_, sockets := connect(t, "/socket.io/", []string{"/", "/admin"})
	for _, s := range sockets {
		events, acks := make(chan string, 1), make(chan string, 1)
		s.OnEvent("echo", func(_ client.Conn, value string) { events <- value })
		s.Emit("echo", "héllo "+s.Namespace(), func(value string) { acks <- value })
		require.Equal(t, "héllo "+s.Namespace(), receive(t, events, "server event"))
		require.Equal(t, "héllo "+s.Namespace(), receive(t, acks, "ACK"))
	}
}

func TestBinaryPayloads(t *testing.T) {
	_, sockets := connect(t, "/socket.io/", []string{"/"})
	payload := []byte{0, 1, 127, 128, 255}
	events, acks := make(chan []byte, 1), make(chan []byte, 1)
	sockets[0].OnEvent("binary", func(_ client.Conn, value *parser.Buffer) { events <- value.Data })
	sockets[0].Emit("binary", &parser.Buffer{Data: payload}, func(value *parser.Buffer) { acks <- value.Data })
	require.Equal(t, payload, receive(t, events, "binary event"))
	require.Equal(t, payload, receive(t, acks, "binary ACK"))
}

func TestConcurrentEmitsKeepOrder(t *testing.T) {
	const goroutines, emits = 4, 50
	_, sockets := connect(t, "/socket.io/", []string{"/"}, client.WithWriteBufferSize(goroutines*emits+1))
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < emits; i++ {
				sockets[0].Emit("order", g, i)
			}
		}()
	}
	wg.Wait()
	result := make(chan []string, 1)
	sockets[0].Emit("order-result", func(order []string) { result <- order })
	order := receive(t, result, "order ACK")
	require.Len(t, order, goroutines*emits)
	next := make([]int, goroutines)
	for _, entry := range order {
		var g, i int
		_, err := fmt.Sscanf(entry, "%d:%d", &g, &i)
		require.NoError(t, err)
		require.Equal(t, next[g], i, "emits of goroutine %d out of order", g)
		next[g]++
	}
}

func TestPingTimeout(t *testing.T) {
	_, sockets := connect(t, "/nopong/", []string{"/"})
	connected := time.Now()
	reasons := make(chan string, 1)
	sockets[0].OnDisconnect(func(_ client.Conn, reason string) { reasons <- reason })
	require.Equal(t, "ping timeout", receive(t, reasons, "disconnect"))
	require.Less(t, time.Since(connected), 1500*time.Millisecond)
}

// TestGoroutineGateFails leaks a connected client when SOCKETIO_BROWSER_LEAK is set, to
// show that TestMain then fails under wasm.
func TestGoroutineGateFails(t *testing.T) {
	if os.Getenv("SOCKETIO_BROWSER_LEAK") == "" {
		t.Skip("set SOCKETIO_BROWSER_LEAK to leak a client")
	}
	cl, err := client.New(os.Getenv("SOCKETIO_BROWSER_URL"), client.WithTransports(websocket.Default))
	require.NoError(t, err)
	require.NoError(t, cl.Connect())
}
