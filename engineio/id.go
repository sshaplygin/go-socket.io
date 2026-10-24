package engineio

import (
	sessionImpl "github.com/sshaplygin/go-socket.io/engineio/internal/session"
)

// IDGenerator generates identifiers for WithSessionIDGenerator; nil selects random IDs.
type IDGenerator interface {
	// NewID returns an identifier unique among the server's sessions; an empty
	// or duplicate identifier fails the handshake.
	NewID() string
}

// DefaultIDGenerator generates URL-safe cryptographic random identifiers.
// *DefaultIDGenerator implements IDGenerator; NewID returns an empty identifier
// when the random source fails. Its legacy field ID is unused.
type DefaultIDGenerator = sessionImpl.DefaultIDGenerator

// IDGenerator keeps the method set of the session contract it is passed to.
var (
	_ IDGenerator             = sessionImpl.IDGenerator(nil)
	_ sessionImpl.IDGenerator = IDGenerator(nil)
	_ IDGenerator             = (*DefaultIDGenerator)(nil)
)
