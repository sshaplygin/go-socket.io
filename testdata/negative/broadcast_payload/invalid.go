package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io"
)

var _, _ = sio.NewEvent[string]("e").EmitTo(context.Background(), sio.BroadcastOperator{}, 42)
