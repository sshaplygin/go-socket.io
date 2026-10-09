package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io/experiments/v2-api"
)

var _, _ = sio.NewEvent[string]("e").EmitTo(context.Background(), sio.BroadcastOperator{}, 42)
