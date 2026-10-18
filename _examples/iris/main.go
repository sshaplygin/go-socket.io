// Package main runs a go-socket.io based websocket server with Iris web server.
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

	"github.com/kataras/iris/v12"

	socketio "github.com/sshaplygin/go-socket.io"
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
	app := iris.New()

	server := socketio.NewServer()

	registerChat(server)

	app.HandleMany("GET POST", "/socket.io/{any:path}", iris.FromStd(server))
	app.HandleDir("/", "../asset")

	app.Configure(iris.WithoutPathCorrection)
	if err := app.Build(); err != nil {
		return errors.Join(err, server.Close())
	}
	return serve(ctx, server, &http.Server{Addr: addr, Handler: app})
}
