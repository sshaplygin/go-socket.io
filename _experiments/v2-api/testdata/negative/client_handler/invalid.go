package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io/experiments/v2-api"
)

var _ = sio.NewEvent[string]("e").HandleClient(nil, func(context.Context, sio.Endpoint, int) error { return nil })
