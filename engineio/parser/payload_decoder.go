package parser

import (
	"bufio"
	"encoding/base64"
	"io"
)

type byteReader interface {
	ReadByte() (byte, error)
	io.Reader
}

// PayloadReader lends an entire Engine.IO payload and reports whether it uses
// binary payload framing. ReleasePayloadReader runs after the final packet is
// closed or decoding fails; its error takes precedence. Acquisition errors are
// returned without a release.
type PayloadReader interface {
	AcquirePayloadReader() (io.Reader, bool, error)
	ReleasePayloadReader(error) error
}

// PayloadDecoder reads Engine.IO v3 polling payload packets. Close each packet
// reader before acquiring the next; Close drains unread packet bytes. A decode
// or release failure is terminal: later reader, read and close calls return the
// saved error without reusing or releasing the source again. Acquisition failures
// may be retried because no payload was acquired.
type PayloadDecoder struct {
	terminalErr error
	b64Reader   io.Reader
	limitReader io.LimitedReader
	rawReader   byteReader
	feeder      PayloadReader

	ft            FrameType
	pt            PacketType
	supportBinary bool
}

func (d *PayloadDecoder) NextReader() (FrameType, PacketType, io.ReadCloser, error) {
	if d.terminalErr != nil {
		return 0, 0, nil, d.terminalErr
	}
	if d.rawReader == nil {
		r, supportBinary, err := d.feeder.AcquirePayloadReader()
		if err != nil {
			return 0, 0, nil, err
		}
		br, ok := r.(byteReader)
		if !ok {
			br = bufio.NewReader(r)
		}
		if err := d.setNextReader(br, supportBinary); err != nil {
			return 0, 0, nil, d.sendError(err)
		}
	}

	return d.ft, d.pt, d, nil
}

func (d *PayloadDecoder) Read(p []byte) (int, error) {
	if d.terminalErr != nil {
		return 0, d.terminalErr
	}
	if d.b64Reader != nil {
		return d.b64Reader.Read(p)
	}
	dd, err := d.limitReader.Read(p)
	if d.supportBinary {
		// a binary payload counts bytes (decodePayloadAsBinary of engine.io-parser 2.2.1)
		return dd, err
	}
	// a text payload counts UTF-16 code units: extend the limit by the extra bytes
	unicodeCount := 0
	for i := range p[:dd] {
		b := p[i]
		if b>>3 == 30 {
			// starts with 11110 4 byte unicode char, probably 2 length in JS
			unicodeCount = unicodeCount + 2
		} else if b>>4 == 14 {
			// starts with 1110 3 byte unicode char, probably 2 length in JS
			unicodeCount = unicodeCount + 2
		} else if b>>5 == 6 {
			// starts with 110 2 byte unicode char, , probably 1 length in JS
			unicodeCount = unicodeCount + 1
		}
	}

	d.limitReader.N = d.limitReader.N + int64(unicodeCount)
	return dd, err
}

func (d *PayloadDecoder) Close() error {
	if d.terminalErr != nil {
		return d.terminalErr
	}
	if d.rawReader == nil {
		return nil
	}
	if _, err := io.Copy(io.Discard, d); err != nil {
		return d.sendError(err)
	}
	err := d.setNextReader(d.rawReader, d.supportBinary)
	if err != nil {
		if err != io.EOF {
			return d.sendError(err)
		}
		err = d.sendError(nil)
	}
	return err
}

func (d *PayloadDecoder) setNextReader(r byteReader, supportBinary bool) error {
	var read func(byteReader) (FrameType, PacketType, int64, error)
	if supportBinary {
		read = d.binaryRead
	} else {
		read = d.textRead
	}

	ft, pt, l, err := read(r)
	if err != nil {
		return err
	}

	d.ft = ft
	d.pt = pt
	d.rawReader = r
	d.limitReader.R = r
	d.limitReader.N = l
	d.supportBinary = supportBinary
	if !supportBinary && ft == Binary {
		d.b64Reader = base64.NewDecoder(base64.StdEncoding, &d.limitReader)
	} else {
		d.b64Reader = nil
	}
	return nil
}

func (d *PayloadDecoder) sendError(err error) error {
	// A released payload must never be read or released again. Retain terminal
	// errors so another call cannot silently acquire or reuse a failed source.
	d.rawReader = nil
	d.limitReader = io.LimitedReader{}
	d.b64Reader = nil
	if e := d.feeder.ReleasePayloadReader(err); e != nil {
		err = e
	}
	d.terminalErr = err
	return err
}

func (d *PayloadDecoder) textRead(r byteReader) (FrameType, PacketType, int64, error) {
	l, err := readTextLen(r)
	if err != nil {
		return 0, 0, 0, err
	}

	ft := String
	b, err := r.ReadByte()
	if err != nil {
		return 0, 0, 0, err
	}
	l--

	if b == 'b' {
		ft = Binary
		b, err = r.ReadByte()
		if err != nil {
			return 0, 0, 0, err
		}
		l--
	}

	pt := ByteToPacketType(b, String)
	return ft, pt, l, nil
}

func (d *PayloadDecoder) binaryRead(r byteReader) (FrameType, PacketType, int64, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, 0, 0, err
	}
	if b > 1 {
		return 0, 0, 0, errInvalidPayload
	}
	ft := ByteToFrameType(b)

	l, err := readBinaryLen(r)
	if err != nil {
		return 0, 0, 0, err
	}

	b, err = r.ReadByte()
	if err != nil {
		return 0, 0, 0, err
	}
	pt := ByteToPacketType(b, ft)
	l--

	return ft, pt, l, nil
}

// NewPayloadDecoder binds a payload source. Each acquired reader is released after its last packet or a decoding failure.
func NewPayloadDecoder(r PayloadReader) *PayloadDecoder { return &PayloadDecoder{feeder: r} }
