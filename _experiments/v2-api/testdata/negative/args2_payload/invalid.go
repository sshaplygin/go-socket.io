package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io/experiments/v2-api"
)

var _ = sio.NewEvent[sio.Args2[string, int]]("e").Emit(context.Background(), nil, sio.Args2[int, string]{})
