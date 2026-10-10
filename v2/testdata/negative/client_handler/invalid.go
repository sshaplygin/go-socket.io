package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io/v2"
)

var _ = sio.NewEvent[string]("e").HandleClient(nil, func(context.Context, sio.Endpoint, int) error { return nil })
