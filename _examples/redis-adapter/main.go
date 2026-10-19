package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	socketio "github.com/sshaplygin/go-socket.io"
	redisadapter "github.com/sshaplygin/go-socket.io/adapter/redis"
)

func main() {
	addr := flag.String("addr", ":8000", "HTTP listen address")
	redisAddr := flag.String("redis", "127.0.0.1:6379", "Redis address or Unix socket")
	prefix := flag.String("prefix", "socket.io", "Redis cluster channel prefix")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *addr, *redisAddr, *prefix); err != nil {
		log.Print(err)
	}
}

func run(ctx context.Context, addr, redisAddr, prefix string) error {
	router := gin.New()

	server := socketio.NewServer(socketio.WithAdapter(redisadapter.New(redisadapter.WithAddr(redisAddr), redisadapter.WithPrefix(prefix))))

	registerChat(server)

	router.GET("/socket.io/*any", gin.WrapH(server))
	router.POST("/socket.io/*any", gin.WrapH(server))
	router.StaticFS("/public", http.Dir("../asset"))

	return serve(ctx, server, &http.Server{Addr: addr, Handler: router})
}
