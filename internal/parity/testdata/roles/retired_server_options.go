package main

import (
	socketio "github.com/sshaplygin/go-socket.io"
)

func main() { _ = socketio.NewServer(&socketio.Options{}) }
