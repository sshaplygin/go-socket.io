package socketio

import (
	"context"

	"github.com/sshaplygin/go-socket.io/experiments/v2-api/parser"
)

type Room string

// SocketID aliases Room so a socket ID can select its automatic room.
type SocketID = Room

// Socket and Namespace have no session or handler storage in this compile proof.
type Socket struct{ id SocketID }
type Namespace struct{ name string }
type Server struct{}

var _ Endpoint = (*Socket)(nil)

func NewServer(Options) (*Server, error)                        { return nil, ErrNotImplemented }
func (*Server) Namespace(name string) *Namespace                { return &Namespace{name: name} }
func (*Server) Shutdown(context.Context) error                  { return ErrNotImplemented }
func (*Server) Close() error                                    { return ErrNotImplemented }
func (s *Socket) ID() SocketID                                  { return s.id }
func (*Socket) SendPacket(context.Context, parser.Packet) error { return ErrNotImplemented }
func (*Socket) RequestAck(context.Context, parser.Packet) (parser.Arguments, error) {
	return parser.Arguments{}, ErrNotImplemented
}
func (*Socket) Join(...Room) error        { return ErrNotImplemented }
func (*Socket) Leave(...Room) error       { return ErrNotImplemented }
func (*Namespace) Use(Middleware) error   { return ErrNotImplemented }
func (*Namespace) OnRaw(RawHandler) error { return ErrNotImplemented }
func (n *Namespace) Name() string         { return n.name }

// BroadcastResult reports local queue acceptance and broker publication only.
type BroadcastResult struct {
	LocalRecipients int
	Published       bool
}

// BroadcastOperator is an immutable selection builder; construction does no I/O.
// Except takes rooms, including the automatic room named by a socket's ID.
type BroadcastOperator struct {
	rooms  []Room
	except []Room
	local  bool
}

func (*Namespace) To(rooms ...Room) BroadcastOperator {
	return BroadcastOperator{rooms: append([]Room(nil), rooms...)}
}
func (b BroadcastOperator) Except(rooms ...Room) BroadcastOperator {
	b.except = append(append([]Room(nil), b.except...), rooms...)
	return b
}
func (b BroadcastOperator) Local() BroadcastOperator { b.local = true; return b }
