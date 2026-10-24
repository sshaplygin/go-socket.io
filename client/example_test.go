package client_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
)

func ExampleNew() {
	server := socketio.NewServer()
	server.OnConnect("/", func(c socketio.Conn) error { return nil })
	server.OnEvent("/", "echo", func(_ socketio.Conn, value string) string {
		return value
	})
	served := make(chan error, 1)
	go func() { served <- server.Serve() }()
	mux := http.NewServeMux()
	mux.Handle("/socket.io/", server)
	httpServer := httptest.NewServer(mux)
	defer func() {
		if err := server.Close(); err != nil {
			panic(err)
		}
		httpServer.Close()
		if err := <-served; err != nil {
			panic(err)
		}
	}()

	cl, err := client.New(httpServer.URL)
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := cl.Close(); err != nil {
			panic(err)
		}
	}()
	cl.OnConnect(func(c client.Conn) error { return nil })
	if err := cl.Connect(); err != nil {
		panic(err)
	}
	replies := make(chan string, 1)
	cl.Emit("echo", "hello", func(value string) { replies <- value })
	select {
	case value := <-replies:
		fmt.Println(value)
	case <-time.After(5 * time.Second):
		panic("echo acknowledgement timed out")
	}
	// Output: hello
}
