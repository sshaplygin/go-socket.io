package session

// ReadyState is the state of a session.
type ReadyState string

const (
	StateOpening ReadyState = "opening" // The OPEN packet has not been written.
	StateOpen    ReadyState = "open"    // The OPEN packet is written; no close has started.
	StateClosing ReadyState = "closing" // A close has started; the transport is not closed yet.
	StateClosed  ReadyState = "closed"  // The transport connection is closed.
)

// ReadyState returns the current session state.
func (s *Session) ReadyState() ReadyState {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()
	switch {
	case s.transportClosed:
		return StateClosed
	case s.closed || s.cause.Load() != nil:
		return StateClosing
	case s.opened.IsZero():
		return StateOpening
	default:
		return StateOpen
	}
}

// Upgraded reports whether the session completed a transport upgrade.
func (s *Session) Upgraded() bool {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()
	return s.upgraded
}
