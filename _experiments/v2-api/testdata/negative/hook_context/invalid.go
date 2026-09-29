package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io/experiments/v2-api"
)

var _ = sio.Hooks{EventStart: func(context.Context, sio.EventInfo) {}}
