package engineio

import (
	"crypto/rand"
	"encoding/base64"
)

// IDGenerator generates a new session id. The default uses cryptographic randomness.
// If you need custom session id, for example using local ip as prefix, you can
// set Options.SessionIDGenerator. The server uses it to generate session IDs.
type IDGenerator interface {
	NewID() string
}

// DefaultIDGenerator generates URL-safe random identifiers. NewID returns an empty
// string if the random source fails.
type DefaultIDGenerator struct {
	// ID is unused; identifiers come from the random source.
	ID uint64
}

func (g *DefaultIDGenerator) NewID() string {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
