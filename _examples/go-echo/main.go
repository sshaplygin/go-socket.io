package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/labstack/echo/v4"

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
	server := socketio.NewServer()

	registerChat(server)

	e := echo.New()
	e.HideBanner = true

	e.Static("/", "../asset")
	e.Any("/socket.io/", func(context echo.Context) error {
		server.ServeHTTP(context.Response(), context.Request())
		return nil
	})
	return serve(ctx, server, &http.Server{Addr: addr, Handler: e})
}
