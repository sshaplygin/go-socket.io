package parser

import (
	"io"
)

// FrameReader is the reader which supports framing.
type FrameReader interface {
	NextReader() (FrameType, io.ReadCloser, error)
}

type PacketDecoder struct {
	r FrameReader
}

func NewPacketDecoder(r FrameReader) *PacketDecoder {
	return &PacketDecoder{
		r: r,
	}
}

func (e *PacketDecoder) NextReader() (FrameType, PacketType, io.ReadCloser, error) {
	ft, r, err := e.r.NextReader()
	if err != nil {
		return 0, 0, nil, err
	}
	var b [1]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		_ = r.Close()
		return 0, 0, nil, err
	}
	return ft, ByteToPacketType(b[0], ft), r, nil
}
