package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io/v2"
)

var _ = sio.NewEvent[string]("e").Handle(nil, func(context.Context, *sio.Socket, string) {})
