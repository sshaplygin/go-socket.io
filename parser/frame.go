package parser

// FrameType identifies a Socket.IO text or binary frame.
type FrameType byte

const (
	// Text is the frame of a packet header and its JSON payload.
	Text FrameType = iota
	// Binary is the frame of one binary attachment.
	Binary
)
