// Package packet supplies observer-only packet identities; it implements no codec.
package packet

// Type and constants mirror the existing Engine.IO packet package.
type Type int

const (
	OPEN Type = iota
	CLOSE
	PING
	PONG
	MESSAGE
	UPGRADE
	NOOP
)
