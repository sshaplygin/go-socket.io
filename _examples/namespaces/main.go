package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"

	socketio "github.com/sshaplygin/go-socket.io"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8000", "HTTP listen address")
	flag.Parse()
	server := socketio.NewServer(nil)
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
	go func() {
		if err := server.Serve(); err != nil {
			log.Print(err)
		}
	}()
	defer server.Close()
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Listening on http://%s\n", listener.Addr())
	log.Fatal(http.Serve(listener, server))
}
