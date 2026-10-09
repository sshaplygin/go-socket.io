package negative

import (
	sio "github.com/sshaplygin/go-socket.io/experiments/v2-api"
	"github.com/sshaplygin/go-socket.io/experiments/v2-api/engineio"
)

var _ = sio.Options{Hooks: &engineio.Hooks{}}
