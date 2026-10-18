package main

import (
	socketio "github.com/sshaplygin/go-socket.io"
)

func main() { _, _ = socketio.NewClient("http://127.0.0.1", nil) }
