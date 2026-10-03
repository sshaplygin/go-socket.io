package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io/experiments/v2-api"
)

var _, _ = sio.NewAckEvent[string, int]("e").EmitWithAck(context.Background(), nil, 42)
