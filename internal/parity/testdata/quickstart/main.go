package main

import (
	"log"
	"net/http"

	socketio "github.com/sshaplygin/go-socket.io"
)

func main() {
	server := socketio.NewServer()
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
			log.Fatal(err)
		}
	}()

	http.Handle("/socket.io/", server)
	err := http.ListenAndServe(":8000", nil)
	_ = server.Close()
	log.Fatal(err)
}
