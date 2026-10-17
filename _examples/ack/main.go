package main

import (
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
	server.OnConnect("/", func(c socketio.Conn) error { return nil })
	server.OnEvent("/", "sum", func(_ socketio.Conn, a, b int) int { return a + b })
	// AckFunc may be saved and called after the handler returns.
	server.OnEvent("/", "deferred", func(_ socketio.Conn, value string, ack socketio.AckFunc) {
		go ack("received: " + value)
	})
	go func() {
		if err := server.Serve(); err != nil {
			log.Print(err)
		}
	}()
	defer server.Close()
	mux := http.NewServeMux()
	mux.Handle("/socket.io/", server)
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Listening on http://%s\n", listener.Addr())
	log.Fatal(http.Serve(listener, mux))
}
