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
	"github.com/sshaplygin/go-socket.io/parser"
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
	server.OnConnect("/", func(c socketio.Conn) error {
		c.Emit("binary-question", &parser.Buffer{Data: []byte{0, 255, 2}}, func(reply *parser.Buffer) {
			c.Emit("binary-confirmed", reply)
		})
		return nil
	})
	// *parser.Buffer preserves binary attachments; []byte alone is JSON data.
	server.OnEvent("/", "binary", func(_ socketio.Conn, value *parser.Buffer) *parser.Buffer { return value })

	mux := http.NewServeMux()
	mux.Handle("/socket.io/", server)
	return serve(ctx, server, &http.Server{Addr: addr, Handler: mux})
}
