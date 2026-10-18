package session

import (
	"crypto/rand"
	"encoding/base64"
)

// IDGenerator generates a new session id. The default uses cryptographic randomness.
// If you need custom session id, for example using local ip as prefix, you can
// implement SessionIDGenerator and save in Configure. Engine.io will use custom
// one to generate new session id.
type IDGenerator interface {
	NewID() string
}

// DefaultIDGenerator generates URL-safe random identifiers. NewID returns an empty
// string if the random source fails.
type DefaultIDGenerator struct {
	// ID is retained for source compatibility; random identifiers do not use it.
	ID uint64
}

func (g *DefaultIDGenerator) NewID() string {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
