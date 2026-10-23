package engineio

import (
	sessionImpl "github.com/sshaplygin/go-socket.io/engineio/internal/session"
)

// IDGenerator generates identifiers for WithSessionIDGenerator; nil selects random IDs.
// IDs must be nonempty and unique. A duplicate fails the handshake.
type IDGenerator = sessionImpl.IDGenerator

// DefaultIDGenerator generates URL-safe cryptographic random identifiers.
// NewID returns an empty identifier when the random source fails.
type DefaultIDGenerator = sessionImpl.DefaultIDGenerator
