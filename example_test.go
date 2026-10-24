package socketio_test

import (
	"log"
	"net"
	"net/http"

	socketio "github.com/sshaplygin/go-socket.io"
)

func ExampleNewServer() {
	server := socketio.NewServer()
	defer func() { _ = server.Close() }()
	server.OnConnect("/", func(c socketio.Conn) error { return nil })
	server.OnEvent("/", "echo", func(_ socketio.Conn, value string) string {
		return value
	})

	mux := http.NewServeMux()
	mux.Handle("/socket.io/", server)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Print(err)
		return
	}
	defer func() { _ = listener.Close() }()

	go func() {
		if err := server.Serve(); err != nil {
			log.Print(err)
		}
	}()
	if err := http.Serve(listener, mux); err != nil {
		log.Print(err)
	}
}
