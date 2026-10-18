package main

import (
	"net/http"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/parser"
)

var (
	_ func(*socketio.Server, string, func(socketio.Conn) error)                  = (*socketio.Server).OnConnect
	_ func(*client.Client, func(client.Conn) error)                              = (*client.Client).OnConnect
	_ func(*engineio.Server) (engineio.Conn, error)                              = (*engineio.Server).Accept
	_ func(*engineclient.Dialer, string, http.Header) (engineclient.Conn, error) = (*engineclient.Dialer).Dial
	_ parser.FrameType                                                           = parser.Text
	_ eioparser.FrameType                                                        = eioparser.String
)

func main() {
	server := socketio.NewServer(&socketio.Options{
		Engine:          &engineio.Options{PingInterval: 25 * time.Second, PingTimeout: 20 * time.Second},
		WriteBufferSize: 64, CloseTimeout: time.Minute,
	})
	defer server.Close()
	server.OnConnect("/", func(socketio.Conn) error { return nil })
	socket, err := client.New("http://127.0.0.1:8000", &client.Options{WriteBufferSize: 64, CloseTimeout: time.Minute})
	if err != nil {
		panic(err)
	}
	defer socket.Close()
	socket.OnConnect(func(client.Conn) error { return nil })
}
