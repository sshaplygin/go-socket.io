package main

import (
	"log"
	"net/http"
	"os"

	socketio "github.com/sshaplygin/go-socket.io"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	server := socketio.NewServer()
	defer func() { _ = server.Close() }()
	root := server.Of("/")
	root.OnEvent("join", func(s socketio.Conn, name string) string {
		s.Join("lobby")
		if err := root.To("lobby").Emit("joined", name); err != nil {
			log.Print(err)
		}
		return "welcome, " + name // sent as the ACK
	})
	go func() {
		if err := server.Serve(); err != nil {
			log.Print(err)
		}
	}()
	http.Handle("/socket.io/", server)
	return http.ListenAndServe(":8000", nil)
}
