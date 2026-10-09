package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io"
)

var _ = sio.NewEvent[string]("e").Handle(nil, func(context.Context, *sio.Socket, int) error { return nil })
