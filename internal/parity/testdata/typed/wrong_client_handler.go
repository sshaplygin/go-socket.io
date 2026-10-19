package consumer

import (
	"context"
	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
)

func reject(c *client.Client) {
	_ = socketio.NewEvent[int]("value").HandleClient(c, func(context.Context, socketio.Endpoint, string) error { return nil })
}
