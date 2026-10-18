package main

import (
	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
)

var (
	_ func(*socketio.Server, string, func(socketio.Conn) error)    = (*socketio.Server).OnConnect
	_ func(*socketio.ServerNamespace, func(socketio.Conn, string)) = (*socketio.ServerNamespace).OnDisconnect
	_ func(socketio.Conn) socketio.Handshake                       = socketio.Conn.Handshake
	_ func(socketio.Conn) engineio.Conn                            = socketio.Conn.EngineConn
	_ socketio.AckFunc                                             = func(...interface{}) {}
)

func main() {
	server := socketio.NewServer()
	defer server.Close()
	server.Of("/chat").OnConnect(func(c socketio.Conn) error { c.Join("lobby"); return nil })
	server.Of("/chat").On("event", func(c socketio.Conn, n int, ack socketio.AckFunc) { ack(n) })
}
