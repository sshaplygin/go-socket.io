package socketio_test

import (
	"net/http/httptest"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/stretchr/testify/require"
)

// TestDefaultQueueAdmitsConnectBurstOverPolling sends a burst, such as chat history,
// from OnConnect with the default queue. Polling drains the queue only on a GET, so
// the burst is queued at once; every event must arrive and the connection stay open.
func TestDefaultQueueAdmitsConnectBurstOverPolling(t *testing.T) {
	const events = 1000
	srv := socketio.NewServer()
	srv.OnConnect("/", func(c socketio.Conn) error {
		for i := 0; i < events; i++ {
			c.Emit("history", i)
		}
		return nil
	})
	failed := make(chan error, 2)
	srv.OnError("/", func(_ socketio.Conn, err error) { failed <- err })
	srv.OnEvent("/", "check", func(socketio.Conn) string { return "open" })
	ts := httptest.NewServer(srv)
	served := make(chan error, 1)
	go func() { served <- srv.Serve() }()
	t.Cleanup(func() {
		require.NoError(t, srv.Close())
		ts.Close()
		require.NoError(t, <-served)
	})

	cl, err := client.New(ts.URL, client.WithTransports(polling.Default), client.WithReconnection(false))
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	received, done := make([]int, 0, events), make(chan client.Conn, 1)
	cl.OnEvent("history", func(c client.Conn, i int) {
		if received = append(received, i); len(received) == events {
			done <- c
		}
	})
	disconnected := make(chan string, 1)
	cl.OnDisconnect(func(_ client.Conn, reason string) { disconnected <- reason })
	require.NoError(t, cl.Connect())

	var conn client.Conn
	select {
	case conn = <-done:
	case err := <-failed:
		t.Fatalf("server error after the client received some of %d events: %v", events, err)
	case reason := <-disconnected:
		t.Fatalf("client disconnected (%s) before receiving %d events", reason, events)
	case <-time.After(20 * time.Second):
		t.Fatalf("the client did not receive %d events", events)
	}
	for i, v := range received {
		require.Equal(t, i, v, "event order")
	}
	acked := make(chan string, 1)
	conn.Emit("check", func(reply string) { acked <- reply })
	select {
	case reply := <-acked:
		require.Equal(t, "open", reply)
	case err := <-failed:
		t.Fatalf("server error after the burst: %v", err)
	case reason := <-disconnected:
		t.Fatalf("client disconnected after the burst: %s", reason)
	case <-time.After(10 * time.Second):
		t.Fatal("the connection did not answer after the burst")
	}
	require.Equal(t, 1, srv.Count(), "the connection stays open")
}
