package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
)

// Each example is a standalone module. Keep this file identical to default-http/lifecycle.go.
func serve(ctx context.Context, sockets *socketio.Server, endpoints ...*http.Server) error {
	return serveAfter(ctx, sockets, 0, endpoints...)
}

// serveAfter delays a signal-triggered shutdown only. A service failure always stops immediately.
func serveAfter(ctx context.Context, sockets *socketio.Server, delay time.Duration, endpoints ...*http.Server) (err error) {
	var listeners []net.Listener
	results := make(chan error, len(endpoints)+1)
	remaining := 0
	defer func() {
		// Keep HTTP available while Socket.IO sends its final disconnects.
		err = errors.Join(err, sockets.Close())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, endpoint := range endpoints {
			err = errors.Join(err, endpoint.Shutdown(shutdownCtx), endpoint.Close())
		}
		for _, listener := range listeners {
			if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
				err = errors.Join(err, closeErr)
			}
		}
		// Every serving goroutine reports once, even when another service failed first.
		for ; remaining > 0; remaining-- {
			err = errors.Join(err, <-results)
		}
	}()

	for _, endpoint := range endpoints {
		listener, listenErr := net.Listen("tcp", endpoint.Addr)
		if listenErr != nil {
			return fmt.Errorf("listen %s: %w", endpoint.Addr, listenErr)
		}
		listeners = append(listeners, listener)
	}
	remaining = len(endpoints) + 1
	go func() {
		serveErr := sockets.Serve()
		if serveErr != nil {
			serveErr = fmt.Errorf("Socket.IO serve: %w", serveErr)
		}
		results <- serveErr
	}()
	for i, endpoint := range endpoints {
		listener := listeners[i]
		fmt.Printf("Listening on http://%s\n", listener.Addr())
		go func() {
			serveErr := endpoint.Serve(listener)
			if errors.Is(serveErr, http.ErrServerClosed) {
				serveErr = nil
			} else if serveErr != nil {
				serveErr = fmt.Errorf("HTTP serve: %w", serveErr)
			}
			results <- serveErr
		}()
	}
	select {
	case err = <-results:
		remaining--
	case <-ctx.Done():
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case err = <-results:
				remaining--
			case <-timer.C:
			}
		}
	}
	return err
}
