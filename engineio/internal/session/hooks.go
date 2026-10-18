package session

import (
	"net/http"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// Hooks are typed observations installed by the owning Engine.IO server before
// startup. Nil callbacks are ignored; this is not an application entry point.
type Hooks struct {
	// Headers observes a session response after the transport prepares its headers
	// and before they are written. The engine server installs it before startup;
	// nil leaves an already installed response observer unchanged.
	Headers      func(r *http.Request, h http.Header)
	Packet       func(s *Session, pt eioparser.PacketType, ft eioparser.FrameType)
	PacketCreate func(s *Session, pt eioparser.PacketType, ft eioparser.FrameType)
	Heartbeat    func(s *Session)
	Upgrading    func(s *Session, transport string)
	Upgrade      func(s *Session, transport string)
	Flush        func(s *Session)
	Drain        func(s *Session)
	Error        func(s *Session, err error)
}

// SetHooks installs h; the engine server calls it after New and before Init,
// on the goroutine that created the session, so every event of the session is observed.
func (s *Session) SetHooks(h *Hooks) {
	if h != nil && h.Headers != nil {
		s.headers = h.Headers
	}
	s.hooks = h
}
