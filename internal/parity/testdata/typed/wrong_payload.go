package consumer

import (
	"context"
	socketio "github.com/sshaplygin/go-socket.io"
)

func reject(c socketio.Conn) {
	_ = socketio.NewEvent[int]("value").Emit(context.Background(), c, "wrong")
}
