package session

import (
	"net/http"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

// Hooks is the socket-level half of engineio.Hooks over *Session: engineio.NewServer
// wraps each set field of engineio.Options.Hooks into the field of the same name. A nil
// field is ignored. Plumbing for the engine server, not user API.
type Hooks struct {
	// Headers observes a session response after the transport prepares its headers
	// and before they are written. The engine server installs it before startup.
	Headers      func(r *http.Request, h http.Header)
	Packet       func(s *Session, pt packet.Type, ft frame.Type)
	PacketCreate func(s *Session, pt packet.Type, ft frame.Type)
	Heartbeat    func(s *Session)
	Upgrading    func(s *Session, transport string)
	Upgrade      func(s *Session, transport string)
	Flush        func(s *Session)
	Drain        func(s *Session)
	Error        func(s *Session, err error)
}

// SetHooks installs h; the engine server calls it after New and before InitSession,
// on the goroutine that created the session, so every event of the session is observed.
func (s *Session) SetHooks(h *Hooks) { s.hooks = h }
