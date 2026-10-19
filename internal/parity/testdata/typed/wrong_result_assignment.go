package consumer

import (
	"context"
	socketio "github.com/sshaplygin/go-socket.io"
)

func reject(c socketio.Conn) {
	var result int
	result, _ = socketio.NewAckEvent[int, string]("value").EmitWithAck(context.Background(), c, 1)
	_ = result
}
