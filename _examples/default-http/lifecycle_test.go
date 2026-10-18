package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/client"
)

func unusedAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func assertStopped(t *testing.T, server *socketio.Server, addr string) {
	t.Helper()
	accepted := make(chan error, 1)
	go func() { _, err := server.Engine().Accept(); accepted <- err }()
	select {
	case err := <-accepted:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("engine still accepts: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("engine Accept did not stop")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listener not released: %v", err)
	}
	_ = listener.Close()
}

func TestListenerFailureStopsEngineAndEarlierListeners(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	server := socketio.NewServer()
	defer server.Close()
	addr := unusedAddress(t)
	err = serve(context.Background(), server,
		&http.Server{Addr: addr},
		&http.Server{Addr: occupied.Addr().String()},
	)
	var netErr *net.OpError
	if !errors.As(err, &netErr) || netErr.Op != "listen" {
		t.Fatalf("listener failure lost: %v", err)
	}
	assertStopped(t, server, addr)
}

func TestServeFailureStopsHTTPAndEngine(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "running", true: "shutdown delay"}[cancelled], func(t *testing.T) {
			server := socketio.NewServer()
			defer server.Close()
			failure := errors.New("adapter unavailable")
			server.SetAdapter(func(string) (adapter.Adapter, error) { return nil, failure })
			server.Of("/failed")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelled {
				cancel()
			}
			addr := unusedAddress(t)
			done := make(chan error, 1)
			go func() { done <- serveAfter(ctx, server, time.Hour, &http.Server{Addr: addr}) }()
			select {
			case err := <-done:
				if !errors.Is(err, failure) {
					t.Fatalf("serve failure lost: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("serve failure did not stop services immediately")
			}
			assertStopped(t, server, addr)
		})
	}
}

func TestSignalStopsLiveChatAndWaitsForDisconnect(t *testing.T) {
	server := socketio.NewServer()
	defer server.Close()
	registerChat(server)
	disconnected := make(chan struct{}, 1)
	server.OnDisconnect("/", func(socketio.Conn, string) { disconnected <- struct{}{} })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := unusedAddress(t)
	done := make(chan error, 1)
	go func() { done <- serve(ctx, server, &http.Server{Addr: addr, Handler: server}) }()
	var peer *client.Client
	waitFor(t, "chat listener", func() bool {
		var err error
		peer, err = client.New("http://"+addr, client.WithTimeout(time.Second), client.WithReconnection(false))
		if err != nil {
			t.Fatal(err)
		}
		if err := peer.Connect(); err != nil {
			_ = peer.Close()
			return false
		}
		return true
	})
	defer peer.Close()
	waitFor(t, "live chat connection", func() bool { return server.Count() == 1 })
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not wait and return")
	}
	select {
	case <-disconnected:
	default:
		t.Fatal("shutdown returned before disconnect callback")
	}
	if server.Count() != 0 {
		t.Fatalf("sessions remain: %d", server.Count())
	}
	assertStopped(t, server, addr)
}

func TestSignalPreservesGracefulDelay(t *testing.T) {
	server := socketio.NewServer()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	const delay = 30 * time.Millisecond
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- serveAfter(ctx, server, delay, &http.Server{Addr: "127.0.0.1:0"}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("graceful shutdown did not finish")
	}
	if time.Since(start) < delay {
		t.Fatal("signal skipped graceful delay")
	}
}

func TestShutdownWaitsForActiveHTTPHandler(t *testing.T) {
	server := socketio.NewServer()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, finished, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defer close(finished)
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	addr := unusedAddress(t)
	done := make(chan error, 1)
	go func() { done <- serve(ctx, server, &http.Server{Addr: addr, Handler: handler}) }()
	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		for {
			response, err := client.Get("http://" + addr)
			if err == nil {
				requestDone <- response.Body.Close()
				return
			}
			select {
			case <-ctx.Done():
				requestDone <- err
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP handler did not start")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("shutdown returned with an active HTTP handler: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish after handler returned")
	}
	select {
	case <-finished:
	default:
		t.Fatal("HTTP handler still active")
	}
	if err := <-requestDone; err != nil {
		t.Fatalf("HTTP request failed during orderly shutdown: %v", err)
	}
	assertStopped(t, server, addr)
}
