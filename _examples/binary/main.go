package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/parser"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8000", "HTTP listen address")
	flag.Parse()
	server := socketio.NewServer(nil)
	server.OnConnect("/", func(c socketio.Conn) error {
		c.Emit("binary-question", &parser.Buffer{Data: []byte{0, 255, 2}}, func(reply *parser.Buffer) {
			c.Emit("binary-confirmed", reply)
		})
		return nil
	})
	// *parser.Buffer preserves binary attachments; []byte alone is JSON data.
	server.OnEvent("/", "binary", func(_ socketio.Conn, value *parser.Buffer) *parser.Buffer { return value })
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
