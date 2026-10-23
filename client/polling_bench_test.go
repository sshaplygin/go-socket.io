package client_test

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

func BenchmarkPollingClientsBroadcast(b *testing.B) {
	b.Run("clients-10", func(b *testing.B) {
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		srv := socketio.NewServer(socketio.WithLogger(log))
		srv.OnConnect("/", func(socketio.Conn) error { return nil })
		var dials atomic.Int64
		ts := httptest.NewUnstartedServer(srv)
		ts.Config.ConnState = func(_ net.Conn, state http.ConnState) {
			if state == http.StateNew {
				dials.Add(1)
			}
		}
		ts.Start()
		served := make(chan struct{})
		go func() { defer close(served); _ = srv.Serve() }()
		clients := make([]*client.Client, 0, 10)
		stop := make(chan struct{})
		received := make(chan struct{}, 10)
		timer := time.NewTimer(5 * time.Minute)
		b.Cleanup(func() {
			close(stop)
			for _, c := range clients {
				_ = c.Close()
			}
			_ = srv.Close()
			ts.Close()
			<-served
			timer.Stop()
			polling.Default.Client.CloseIdleConnections()
		})
		for i := 0; i < 10; i++ {
			c, err := client.New(ts.URL, client.WithTransports(polling.Default), client.WithReconnection(false), client.WithLogger(log))
			if err != nil {
				b.Fatal(err)
			}
			clients = append(clients, c)
			c.OnEvent("data", func(client.Conn, string) {
				select {
				case received <- struct{}{}:
				case <-stop:
				}
			})
			if err := c.Connect(); err != nil {
				b.Fatal(err)
			}
		}
		before := dials.Load()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if !srv.BroadcastToNamespace("/", "data", "hello") {
				b.Fatal("broadcast rejected")
			}
			for j := 0; j < 10; j++ {
				select {
				case <-received:
				case <-timer.C:
					b.Fatal("broadcast delivery timeout")
				}
			}
		}
		b.StopTimer()
		if os.Getenv("SOCKETIO_BENCH_METRICS") == "1" {
			b.ReportMetric(float64(dials.Load()-before)/float64(b.N), "dials/op")
		}
	})
}
