package main

import (
	"net/http"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/parser"
)

var (
	_ func(...socketio.Option) *socketio.Server                                                  = socketio.NewServer
	_ func(...engineio.Option) *engineio.Server                                                  = engineio.NewServer
	_ func(string, ...client.Option) (*client.Client, error)                                     = client.New
	_ func(...transport.Option) *transport.Manager                                               = transport.NewManager
	_ func(eioparser.PayloadWriter, ...eioparser.PayloadEncoderOption) *eioparser.PayloadEncoder = eioparser.NewPayloadEncoder
	_ func(*socketio.Server, string, func(socketio.Conn) error)                                  = (*socketio.Server).OnConnect
	_ func(*client.Client, func(client.Conn) error)                                              = (*client.Client).OnConnect
	_ func(*engineio.Server) (engineio.Conn, error)                                              = (*engineio.Server).Accept
	_ func(*engineclient.Dialer, string, http.Header) (engineclient.Conn, error)                 = (*engineclient.Dialer).Dial
	_ parser.FrameType                                                                           = parser.Text
	_ eioparser.FrameType                                                                        = eioparser.String
)

func main() {
	defaultServer := socketio.NewServer()
	defer defaultServer.Close()
	defaultEngine := engineio.NewServer()
	defer defaultEngine.Close()
	defaultClient, err := client.New("http://127.0.0.1:8000")
	if err != nil {
		panic(err)
	}
	defer defaultClient.Close()
	server := socketio.NewServer(socketio.WithEngineOptions(engineio.WithPingInterval(25*time.Second), engineio.WithPingTimeout(20*time.Second)), socketio.WithWriteBufferSize(1024), socketio.WithCloseTimeout(time.Minute))
	defer server.Close()
	server.OnConnect("/", func(socketio.Conn) error { return nil })
	socket, err := client.New("http://127.0.0.1:8000", client.WithWriteBufferSize(64), client.WithCloseTimeout(time.Minute))
	if err != nil {
		panic(err)
	}
	defer socket.Close()
	socket.OnConnect(func(client.Conn) error { return nil })
}
