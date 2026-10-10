package negative

import (
	sio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
)

var _ = sio.ChainHooks(&engineio.Hooks{})
var _ = sio.LoggingHooks("logger")
