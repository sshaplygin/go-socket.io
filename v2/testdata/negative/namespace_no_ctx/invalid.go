package negative

import sio "github.com/sshaplygin/go-socket.io/v2"

// The creating call takes ctx and returns an error.
var _, _ = (&sio.Server{}).Namespace("/")
