// Package frame supplies observer-only frame identities; it implements no I/O.
package frame

// Type and constants mirror the existing Engine.IO frame package.
type Type byte

const (
	String Type = iota
	Binary
)
