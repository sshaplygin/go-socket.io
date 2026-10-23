package engineio

import (
	sessionImpl "github.com/sshaplygin/go-socket.io/engineio/internal/session"
)

// IDGenerator generates identifiers for WithSessionIDGenerator; nil selects random IDs.
type IDGenerator interface {
	// NewID returns the next session identifier.
	NewID() string
}

// DefaultIDGenerator generates URL-safe cryptographic random identifiers.
// NewID() string returns an empty identifier when the random source fails.
// Its legacy field ID uint64 is unused.
type DefaultIDGenerator = sessionImpl.DefaultIDGenerator
