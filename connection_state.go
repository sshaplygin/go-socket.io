package socketio

// connectionState is guarded by connection.mu. Channels publish transitions to
// workers; the enum makes queue admission independent of combinations of them.
// The draining flag remembers an application-initiated close after discard, for
// disconnect callbacks and reasons; it is not the current queue state.
type connectionState uint8

const (
	connectionOpen connectionState = iota
	connectionClosing
	connectionDraining
	connectionSealed
	connectionDiscarded
	connectionClosed
)
