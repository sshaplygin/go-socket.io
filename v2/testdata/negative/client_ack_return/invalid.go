package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io"
)

var _ = sio.NewAckEvent[string, int]("e").HandleClient(nil, func(context.Context, sio.Endpoint, string) (string, error) { return "", nil })
