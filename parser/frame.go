package parser

// FrameType identifies a Socket.IO text or binary frame.
type FrameType byte

const (
	Text FrameType = iota
	Binary
)
