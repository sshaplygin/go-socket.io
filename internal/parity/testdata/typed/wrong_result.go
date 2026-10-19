package consumer

import (
	"context"
	socketio "github.com/sshaplygin/go-socket.io"
)

func reject(s *socketio.Server) {
	_ = socketio.NewAckEvent[int, string]("value").Handle(s.Of("/"), func(context.Context, socketio.Conn, int) (int, error) { return 1, nil })
}
