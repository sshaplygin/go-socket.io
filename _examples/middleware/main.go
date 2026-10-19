package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	socketio "github.com/sshaplygin/go-socket.io"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8000", "HTTP listen address")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *addr); err != nil {
		log.Print(err)
		stop()
		os.Exit(1)
	}
}

func run(ctx context.Context, addr string) error {
	server := socketio.NewServer()
	server.OnConnect("/", func(socketio.Conn) error { return nil })
	server.OnEvent("/", "health", func(socketio.Conn) string { return "pong" })
	secure := server.Of("/secure")
	// Namespace middleware runs before OnConnect and the CONNECT reply.
	secure.Use(func(c socketio.Conn) error {
		if c.Handshake().Query.Get("token") != "demo-secret" {
			return errors.New("unauthorized")
		}
		return nil
	})
	secure.Use(func(c socketio.Conn) error {
		c.SetContext(c.Handshake().Query.Get("name"))
		return nil
	})
	secure.OnConnect(func(c socketio.Conn) error {
		// Socket middleware runs before each event handler. Rejection sends ERROR
		// while leaving this namespace and the engine connection open.
		c.Use(func(event string, _ []interface{}) error {
			if event == "blocked" {
				return errors.New("event blocked")
			}
			return nil
		})
		return nil
	})
	secure.OnEvent("echo", func(c socketio.Conn, message string) string { return c.Context().(string) + ": " + message })
	secure.OnEvent("blocked", func(socketio.Conn, string) string { return "handler should not run" })
	secure.OnError(func(_ socketio.Conn, err error) { log.Printf("secure: %v", err) })

	return serve(ctx, server, &http.Server{Addr: addr, Handler: server})
}
