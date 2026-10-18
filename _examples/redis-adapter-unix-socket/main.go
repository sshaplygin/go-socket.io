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

	"github.com/gin-gonic/gin"

	socketio "github.com/sshaplygin/go-socket.io"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *addr); err != nil {
		log.Print(err)
	}
}

func run(ctx context.Context, addr string) error {
	router := gin.New()

	server := socketio.NewServer()

	_, err := server.Adapter(&socketio.RedisAdapterOptions{
		Addr:    "/tmp/docker/redis.sock",
		Network: "unix",
	})
	if err != nil {
		return errors.Join(err, server.Close())
	}

	registerChat(server)

	router.GET("/socket.io/*any", gin.WrapH(server))
	router.POST("/socket.io/*any", gin.WrapH(server))
	router.StaticFS("/public", http.Dir("../asset"))

	return serve(ctx, server, &http.Server{Addr: addr, Handler: router})
}
