package negative

import (
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/parser"
)

var _ = engineio.PacketInfo{Type: parser.Event}
