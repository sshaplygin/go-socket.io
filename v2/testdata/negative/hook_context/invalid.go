package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io/v2"
)

var _ = sio.Hooks{EventStart: func(context.Context, sio.EventInfo) {}}
