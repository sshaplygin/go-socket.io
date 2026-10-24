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
	addr := flag.String("addr", ":8000", "HTTP listen address")
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
	// Defaults accept every origin. Pass
	// socketio.WithEngineOptions(engineio.WithCORS(false)) for same-origin
	// websockets, or explicit transport CheckOrigin callbacks for an allow-list
	// that also accepts an empty Origin, which non-browser clients send.
	// See docs/PROTOCOL.md for polling CORS and request access-control behavior.
	server := socketio.NewServer()

	registerChat(server)

	mux := http.NewServeMux()
	mux.Handle("/socket.io/", server)
	mux.Handle("/", http.FileServer(http.Dir("../asset")))
	return serve(ctx, server, &http.Server{Addr: addr, Handler: mux})
}
