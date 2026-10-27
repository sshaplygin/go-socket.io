package client

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

// A polling GET whose Socket.IO packet fails to decode is still lent to the
// payload decoder when the client closes its transport. The transport must not
// read that body concurrently, and closing the packet reader must not decode
// further from it: the client disconnects, Close returns and no library
// goroutine remains.
func TestMalformedPollingPacketReleasesPayload(t *testing.T) {
	srv := engineio.NewServer(shortPingOptions()...)
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { _ = srv.Close(); ts.Close() })
	cl, err := New(ts.URL, WithTransports(polling.Default), WithReconnection(false))
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	connected, disconnected := make(chan struct{}, 1), make(chan string, 1)
	cl.OnConnect(func(Conn) error { connected <- struct{}{}; return nil })
	cl.OnError(func(Conn, error) {})
	cl.OnDisconnect(func(_ Conn, reason string) { disconnected <- reason })
	require.NoError(t, cl.Connect())
	server, err := srv.Accept()
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Close() })
	send := func(packet string) {
		w, err := server.NextWriter(eioparser.String)
		require.NoError(t, err)
		_, err = io.WriteString(w, packet)
		require.NoError(t, err)
		require.NoError(t, w.Close())
	}
	send("0")
	recv(t, connected, "Socket.IO CONNECT")
	send("9" + strings.Repeat("x", 100000))
	recv(t, disconnected, "disconnect after malformed polling packet")
	closed := make(chan error, 1)
	go func() { closed <- cl.Close() }()
	require.NoError(t, recv(t, closed, "client Close"))
	require.NoError(t, server.Close())
	require.NoError(t, srv.Close())
	require.Eventually(t, func() bool { return len(leakedModuleStacks()) == 0 }, time.Second, 10*time.Millisecond,
		"payload reader or another library worker remains after close")
}
