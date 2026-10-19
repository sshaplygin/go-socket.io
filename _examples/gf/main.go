package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gogf/gf/frame/g"
	"github.com/gogf/gf/net/ghttp"

	socketio "github.com/sshaplygin/go-socket.io"
)

func cors(r *ghttp.Request) {
	r.Response.CORSDefault()
	r.Middleware.Next()
}

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
	s := g.Server()

	server := socketio.NewServer()

	s.BindMiddlewareDefault(cors)
	s.BindHandler("/socket.io/", func(r *ghttp.Request) {
		server.ServeHTTP(r.Response.Writer, r.Request)
	})

	registerChat(server)

	// Own the HTTP server: GoFrame Run/Start can terminate the process on failure.
	return serve(ctx, server, &http.Server{Addr: addr, Handler: s})
}
