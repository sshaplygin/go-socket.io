package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io"
)

var _, _ = sio.NewAckEvent[string, int]("e").EmitWithAck(context.Background(), nil, 42)
