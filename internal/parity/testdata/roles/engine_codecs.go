package main

import (
	"io"

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

func main() {
	writer := parser.NewFakeConnWriter()
	_ = parser.NewPacketEncoder(writer)
	_ = parser.NewPacketDecoder(parser.NewFakeConnReader([]parser.Frame{{FType: parser.String}}))
}
