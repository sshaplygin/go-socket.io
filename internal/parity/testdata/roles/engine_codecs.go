package main

import (
	"io"
	"strings"

	"github.com/sshaplygin/go-socket.io/engineio/parser"
)

var (
	_ func(parser.FrameWriter) *parser.PacketEncoder                                    = parser.NewPacketEncoder
	_ func(parser.FrameReader) *parser.PacketDecoder                                    = parser.NewPacketDecoder
	_ func(parser.PayloadWriter, ...parser.PayloadEncoderOption) *parser.PayloadEncoder = parser.NewPayloadEncoder
	_ func(parser.PayloadReader) *parser.PayloadDecoder                                 = parser.NewPayloadDecoder
	_ func(bool) parser.PayloadEncoderOption                                            = parser.WithBinarySupport
	_ parser.PayloadEncoderOption                                                       = func(*parser.PayloadEncoder) {}
	_ interface {
		NextWriter(parser.FrameType, parser.PacketType) (io.WriteCloser, error)
	} = (*parser.PayloadEncoder)(nil)
	_ interface {
		NextReader() (parser.FrameType, parser.PacketType, io.ReadCloser, error)
	} = (*parser.PayloadDecoder)(nil)
	_ io.ReadCloser  = (*parser.PayloadDecoder)(nil)
	_ io.WriteCloser = (*parser.PayloadEncoder)(nil)
	_ parser.Frame   = parser.Frame{FType: parser.String, Data: []byte("hello")}
	_ parser.Packet  = parser.Packet{FType: parser.Binary, PType: parser.MESSAGE, Data: []byte{1}}
)

type frameWriter struct{}

func (frameWriter) NextWriter(parser.FrameType) (io.WriteCloser, error) { return discardWriter{}, nil }

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriter) Close() error                { return nil }

type frameReader struct{}

func (frameReader) NextReader() (parser.FrameType, io.ReadCloser, error) {
	return parser.String, io.NopCloser(strings.NewReader("4hello")), nil
}

func main() {
	_ = parser.NewPacketEncoder(frameWriter{})
	_ = parser.NewPacketDecoder(frameReader{})
}
