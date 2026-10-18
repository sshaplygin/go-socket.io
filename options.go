package socketio

import (
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
)

// Options configures a Socket.IO server and its underlying Engine.IO server.
type Options struct {
	// Engine configures the underlying server; nil selects Engine.IO defaults.
	Engine *engineio.Options
	// WriteBufferSize bounds queued packets; nonpositive means 64.
	WriteBufferSize int
	// CloseTimeout bounds orderly drain; nonpositive means one minute.
	CloseTimeout time.Duration
}
