package payload

import (
	"bytes"
	"encoding/base64"
	"github.com/sshaplygin/go-socket.io/engineio/parser/internal/packet"
	"io"
	"unicode/utf8"
)

// Writer lends a destination for one encoded packet. ReleasePayloadWriter
// receives the encoding/write error, or nil, after that packet; its error takes
// precedence. AcquirePayloadWriter errors are returned without a release.
type Writer interface {
	AcquirePayloadWriter() (io.Writer, error)
	ReleasePayloadWriter(error) error
}

// Encoder writes Engine.IO v3 polling payload packets. Use one writer at a
// time and close each packet writer before acquiring the next.
type Encoder struct {
	supportBinary bool
	feeder        Writer

	ft         packet.FrameType
	pt         packet.PacketType
	header     bytes.Buffer
	frameCache bytes.Buffer
	b64Writer  io.WriteCloser
	rawWriter  io.Writer
}

func (e *Encoder) NOOP() []byte {
	if e.supportBinary {
		return []byte{0x00, 0x01, 0xff, '6'}
	}
	return []byte("1:6")
}

func (e *Encoder) NextWriter(ft packet.FrameType, pt packet.PacketType) (io.WriteCloser, error) {
	w, err := e.feeder.AcquirePayloadWriter()
	if err != nil {
		return nil, err
	}
	e.rawWriter = w

	e.ft = ft
	e.pt = pt
	e.frameCache.Reset()

	if !e.supportBinary && ft == packet.Binary {
		e.b64Writer = base64.NewEncoder(base64.StdEncoding, &e.frameCache)
	} else {
		e.b64Writer = nil
	}
	return e, nil
}

func (e *Encoder) Write(p []byte) (int, error) {
	if e.b64Writer != nil {
		return e.b64Writer.Write(p)
	}
	// Need to scan here and add unicode chars to the list
	return e.frameCache.Write(p)
}

func (e *Encoder) Close() error {
	if e.b64Writer != nil {
		if err := e.b64Writer.Close(); err != nil {
			return err
		}
	}

	var writeHeader func() error
	if e.supportBinary {
		writeHeader = e.writeBinaryHeader
	} else {
		if e.ft == packet.Binary {
			writeHeader = e.writeB64Header
		} else {
			writeHeader = e.writeTextHeader
		}
	}

	e.header.Reset()
	err := writeHeader()
	if err == nil {
		_, err = e.header.WriteTo(e.rawWriter)
	}
	if err == nil {
		_, err = e.frameCache.WriteTo(e.rawWriter)
	}
	if werr := e.feeder.ReleasePayloadWriter(err); werr != nil {
		return werr
	}
	return err
}

func (e *Encoder) writeTextHeader() error {

	err := writeTextLen(e.calcCodeUnitLength(), &e.header)
	if err == nil {
		err = e.header.WriteByte(e.pt.StringByte())
	}
	return err
}

func (e *Encoder) writeB64Header() error {
	l := int64(utf8.RuneCount(e.frameCache.Bytes()) + 2) // length for 'b' and packet type
	err := writeTextLen(l, &e.header)
	if err == nil {
		err = e.header.WriteByte('b')
	}
	if err == nil {
		err = e.header.WriteByte(e.pt.StringByte())
	}
	return err
}

func (e *Encoder) calcCodeUnitLength() int64 {
	var l int64 = 1
	var codeUnitSize int64
	bytes := e.frameCache.Bytes()
	for i := range bytes {
		b := bytes[i]
		if b>>3 == 30 {
			// starts with 11110 4 byte unicode char, probably 2 length in JS
			codeUnitSize = 2
		} else if b>>4 == 14 {
			// starts with 1110 3 byte unicode char, probably 1 length in JS
			codeUnitSize = 1
		} else if b>>5 == 6 {
			// starts with 110 2 byte unicode char, , probably 1 length in JS
			codeUnitSize = 1
		} else if b>>6 == 2 {
			// starts with 10 just unicode byte
			codeUnitSize = 0
		} else {
			codeUnitSize = 1
		}
		l = l + codeUnitSize
	}

	return int64(l)
}

// writeBinaryHeader writes the header of a packet in a binary payload. The length is
// in bytes, the packet type byte included, for string and binary frames alike, as in
// encodePayloadAsBinary of engine.io-parser 2.2.1 (a string packet is UTF-8 encoded
// there before it is measured).
func (e *Encoder) writeBinaryHeader() error {
	l := int64(e.frameCache.Len()) + 1 // length for packet type
	b := e.pt.StringByte()
	if e.ft == packet.Binary {
		b = e.pt.BinaryByte()
	}
	err := e.header.WriteByte(e.ft.Byte())
	if err == nil {
		err = writeBinaryLen(l, &e.header)
	}
	if err == nil {
		err = e.header.WriteByte(b)
	}
	return err
}

// EncoderOption configures a payload encoder.
type EncoderOption func(*Encoder)

// WithBinarySupport selects binary payload framing. The default is true;
// transports pass the support negotiated with their peer.
func WithBinarySupport(enabled bool) EncoderOption {
	return func(encoder *Encoder) { encoder.supportBinary = enabled }
}

// NewEncoder binds a writer with binary payload support enabled.
func NewEncoder(w Writer, opts ...EncoderOption) *Encoder {
	encoder := &Encoder{feeder: w, supportBinary: true}
	for _, option := range opts {
		if option != nil {
			option(encoder)
		}
	}
	return encoder
}
