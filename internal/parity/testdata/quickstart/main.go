package main

import (
	"log"
	"net/http"

	socketio "github.com/sshaplygin/go-socket.io"
)

func main() {
	server := socketio.NewServer()
	root := server.Of("/")
	root.OnEvent("join", func(s socketio.Conn, room string) string {
		s.Join(room)
		if err := root.To(room).Emit("joined", s.ID()); err != nil {
			log.Print(err)
		}
		return "welcome to " + room // sent as the ACK
	})
	go server.Serve()

	http.Handle("/socket.io/", server)
	err := http.ListenAndServe(":8000", nil)
	server.Close()
	log.Fatal(err)
}
