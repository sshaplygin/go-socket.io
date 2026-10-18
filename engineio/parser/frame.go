package parser

// FrameType is the type of frames.
type FrameType byte

const (
	// String identifies a string frame.
	String FrameType = iota
	// Binary identifies a binary frame.
	Binary
)

// ByteToFrameType converts a byte to FrameType.
func ByteToFrameType(b byte) FrameType {
	return FrameType(b)
}

// Byte returns type in byte.
func (t FrameType) Byte() byte {
	return byte(t)
}
