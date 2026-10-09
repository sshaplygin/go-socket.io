package socketio

import (
	"context"
	"encoding/json"
	"log/slog"
)

// Namespace is a Socket.IO namespace. Server.Namespace is the only call that
// creates one. The skeleton holds no sockets, handlers or adapter.
type Namespace struct{ name string }

// Name returns the namespace name, or "" for a nil Namespace.
func (n *Namespace) Name() string {
	if n == nil {
		return ""
	}
	return n.name
}

// Middleware authenticates a socket before the namespace accepts it. It receives
// the raw credentials of the CONNECT packet.
type Middleware func(context.Context, *Socket, json.RawMessage) error

// Auth adapts a typed credentials handler to a Middleware without reflecting over
// the handler. The skeleton's Middleware always returns ErrNotImplemented.
func Auth[T any](h func(context.Context, *Socket, T) error) Middleware {
	return func(context.Context, *Socket, json.RawMessage) error { return ErrNotImplemented }
}

// Use adds middleware to the namespace. The skeleton always returns ErrNotImplemented.
func (*Namespace) Use(Middleware) error { return ErrNotImplemented }

// OnRaw registers the raw event handler of the namespace. The skeleton always
// returns ErrNotImplemented.
func (*Namespace) OnRaw(RawHandler) error { return ErrNotImplemented }

// BroadcastResult reports what a broadcast achieved: LocalRecipients counts
// successful local enqueues after room union, deduplication and exclusions, and
// Published records that the broker accepted the publication. Neither implies
// remote delivery or a client acknowledgement.
type BroadcastResult struct {
	LocalRecipients int
	Published       bool
}

// BroadcastOperator is an immutable selection builder: every method returns a
// modified copy and none performs I/O. Rooms are the union of the To rooms; an empty
// selection means every socket of the namespace.
type BroadcastOperator struct {
	rooms  []Room
	except []Room
	local  bool
}

// To selects the sockets in any of rooms.
func (*Namespace) To(rooms ...Room) BroadcastOperator {
	return BroadcastOperator{rooms: append([]Room(nil), rooms...)}
}

// Except excludes the sockets in any of rooms, including the automatic room named by
// a socket ID.
func (b BroadcastOperator) Except(rooms ...Room) BroadcastOperator {
	b.except = append(append([]Room(nil), b.except...), rooms...)
	return b
}

// Local restricts the broadcast to this server; a local broadcast never publishes.
func (b BroadcastOperator) Local() BroadcastOperator {
	b.local = true
	return b
}

// Hooks returns the observer hooks of the server that owns the namespace, for
// adapters in other modules. The skeleton has no owner and returns nil, also for a
// nil Namespace; the runtime will return a nil-safe wrapper.
func (*Namespace) Hooks() *Hooks { return nil }

// LocalSockets returns the seam an adapter uses to reach the sockets of this server,
// for adapters in other modules. The skeleton has no sockets and returns nil, also for
// a nil Namespace; the runtime (2.3S) implements it and never returns nil for a
// namespace that Server.Namespace created.
func (*Namespace) LocalSockets() LocalSockets { return nil }

// Logger returns the instance logger of the server that owns the namespace, for
// adapter and contrib diagnostics. The skeleton has no owner and returns nil, also
// for a nil Namespace.
func (*Namespace) Logger() *slog.Logger { return nil }
