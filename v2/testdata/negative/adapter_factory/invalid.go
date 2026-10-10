package negative

import sio "github.com/sshaplygin/go-socket.io/v2"

// The factory takes ctx; the pre-readiness signature must not compile.
var _ sio.AdapterFactory = func(*sio.Namespace) (sio.Adapter, error) { return nil, nil }
