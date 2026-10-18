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
	}
}

func run(ctx context.Context, addr string) error {
	server := socketio.NewServer()
	server.OnConnect("/", func(socketio.Conn) error { return nil })
	chat := server.Of("/chat")
	chat.Use(func(c socketio.Conn) error {
		query := c.Handshake().Query
		if query.Get("token") != "demo-secret" {
			return errors.New("unauthorized")
		}
		c.SetContext(query.Get("name"))
		return nil
	})
	chat.OnConnect(func(socketio.Conn) error { return nil })
	chat.OnEvent("join", func(c socketio.Conn, room string) string { c.Join(room); return room })
	chat.OnEvent("say", func(c socketio.Conn, room, message string) string {
		if err := c.To(room).Emit("message", c.Context().(string), message); err != nil {
			return err.Error()
		}
		return "sent"
	})
	chat.OnEvent("inspect", func(c socketio.Conn) map[string]interface{} {
		return map[string]interface{}{"namespace": c.Namespace(), "id": c.ID(), "rooms": c.Rooms()}
	})
	news := server.Of("/news")
	news.OnConnect(func(socketio.Conn) error { return nil })
	news.OnEvent("echo", func(_ socketio.Conn, message string) string { return "news: " + message })

	return serve(ctx, server, &http.Server{Addr: addr, Handler: server})
}
