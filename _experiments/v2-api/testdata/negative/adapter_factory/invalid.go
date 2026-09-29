package negative

import sio "github.com/sshaplygin/go-socket.io/experiments/v2-api"

var _ sio.AdapterFactory = func(string) (sio.Adapter, error) { return nil, nil }
