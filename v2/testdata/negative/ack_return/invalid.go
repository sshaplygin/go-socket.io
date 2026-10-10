package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io/v2"
)

var _ = sio.NewAckEvent[string, int]("e").Handle(nil, func(context.Context, *sio.Socket, string) (string, error) { return "", nil })
