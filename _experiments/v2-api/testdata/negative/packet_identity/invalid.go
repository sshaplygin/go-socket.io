package negative

import (
	"github.com/sshaplygin/go-socket.io/experiments/v2-api/engineio"
	"github.com/sshaplygin/go-socket.io/experiments/v2-api/parser"
)

var _ = engineio.PacketInfo{Type: parser.Event}
