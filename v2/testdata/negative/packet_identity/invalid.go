package negative

import (
	"github.com/sshaplygin/go-socket.io/v2/engineio"
	"github.com/sshaplygin/go-socket.io/v2/parser"
)

var _ = engineio.PacketInfo{Type: parser.Event}
