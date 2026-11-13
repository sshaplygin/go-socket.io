package main

import (
	"log"
	"net/http"

	socketio "github.com/sshaplygin/go-socket.io"
)

func main() {
	server := socketio.NewServer()
	server.OnEvent("/", "join", func(s socketio.Conn, room string) string {
		s.Join(room)
		server.BroadcastToRoom("/", room, "joined", s.ID())
		return "welcome to " + room // sent as the ACK
	})
	go server.Serve()

	http.Handle("/socket.io/", server)
	err := http.ListenAndServe(":8000", nil)
	server.Close()
	log.Fatal(err)
}
