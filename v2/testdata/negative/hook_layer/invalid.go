package negative

import (
	sio "github.com/sshaplygin/go-socket.io/v2"
	"github.com/sshaplygin/go-socket.io/v2/engineio"
)

var _ = sio.Options{Hooks: &engineio.Hooks{}}
