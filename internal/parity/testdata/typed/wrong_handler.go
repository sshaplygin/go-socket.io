package consumer

import (
	"context"
	socketio "github.com/sshaplygin/go-socket.io"
)

func reject(s *socketio.Server) {
	_ = socketio.NewEvent[int]("value").Handle(s.Of("/"), func(context.Context, socketio.Conn, string) error { return nil })
}
