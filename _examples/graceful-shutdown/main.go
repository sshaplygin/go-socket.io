package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
)

const (
	gracefulDelay = 3 * time.Second
)

func main() {
	addr := flag.String("addr", ":8000", "HTTP listen address")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *addr); err != nil {
		log.Print(err)
	}
}

func run(ctx context.Context, addr string) error {
	server := socketio.NewServer()

	registerChat(server)

	mux := http.NewServeMux()
	mux.Handle("/socket.io/", server)
	mux.Handle("/", http.FileServer(http.Dir("../asset")))
	log.Printf("shutdown delay after signal: %v", gracefulDelay)
	return serveAfter(ctx, server, gracefulDelay, &http.Server{Addr: addr, Handler: mux})
}
