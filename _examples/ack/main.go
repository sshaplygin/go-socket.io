package main

import (
	"context"
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
	}
}

func run(ctx context.Context, addr string) error {
	server := socketio.NewServer()
	server.OnConnect("/", func(c socketio.Conn) error { return nil })
	server.OnEvent("/", "sum", func(_ socketio.Conn, a, b int) int { return a + b })
	// AckFunc may be saved and called after the handler returns.
	server.OnEvent("/", "deferred", func(_ socketio.Conn, value string, ack socketio.AckFunc) {
		go ack("received: " + value)
	})

	mux := http.NewServeMux()
	mux.Handle("/socket.io/", server)
	return serve(ctx, server, &http.Server{Addr: addr, Handler: mux})
}
