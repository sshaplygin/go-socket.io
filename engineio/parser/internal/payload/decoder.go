package payload

import (
	"bufio"
	"encoding/base64"
	"github.com/sshaplygin/go-socket.io/engineio/parser/internal/packet"
	"io"
)

type byteReader interface {
	ReadByte() (byte, error)
	io.Reader
}

// Reader lends an entire Engine.IO payload and reports whether it uses
// binary payload framing. ReleasePayloadReader runs after the final packet is
// closed or decoding fails; its error takes precedence. Acquisition errors are
// returned without a release.
type Reader interface {
	AcquirePayloadReader() (io.Reader, bool, error)
	ReleasePayloadReader(error) error
}

// Decoder reads Engine.IO v3 polling payload packets. Close each packet
// reader before acquiring the next; Close drains unread packet bytes. A decode
// or release failure is terminal: later reader, read and close calls return the
// saved error without reusing or releasing the source again. Non-EOF body read
// failures release the source immediately, including reads returning data and an
// error together. EOF ends the current packet. Acquisition failures
// may be retried because no payload was acquired.
type Decoder struct {
	terminalErr error
	b64Reader   io.Reader
	limitReader io.LimitedReader
	rawReader   byteReader
	feeder      Reader

	ft            packet.FrameType
	pt            packet.PacketType
	supportBinary bool
}

func (d *Decoder) NextReader() (packet.FrameType, packet.PacketType, io.ReadCloser, error) {
	if d.terminalErr != nil {
		return 0, 0, nil, d.terminalErr
	}
	for d.rawReader == nil {
		r, supportBinary, err := d.feeder.AcquirePayloadReader()
		if err != nil {
			return 0, 0, nil, err
		}
		br, ok := r.(byteReader)
		if !ok {
			br = bufio.NewReader(r)
		}
		if err := d.setNextReader(br, supportBinary); err != nil {
			if err == io.EOF && supportBinary {
				// An empty binary POST contains no packets. Release this body
				// normally and wait for the next POST without poisoning decoding.
				if err := d.sendError(nil); err != nil {
					return 0, 0, nil, err
				}
				continue
			}
			return 0, 0, nil, d.sendError(err)
		}
	}

	return d.ft, d.pt, d, nil
}

func (d *Decoder) Read(p []byte) (n int, err error) {
	if d.terminalErr != nil {
		return 0, d.terminalErr
	}
	defer func() {
		if err != nil && err != io.EOF {
			err = d.sendError(err)
		}
	}()
	if d.b64Reader != nil {
		return d.b64Reader.Read(p)
	}
	dd, err := d.limitReader.Read(p)
	if err == io.EOF && d.limitReader.N > 0 {
		return dd, io.ErrUnexpectedEOF
	}
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

func (d *Decoder) Close() error {
	if d.terminalErr != nil {
		return d.terminalErr
	}
	if d.rawReader == nil {
		return nil
	}
	if _, err := io.Copy(io.Discard, d); err != nil {
		return d.sendError(err)
	}
	if d.limitReader.N > 0 {
		return d.sendError(io.ErrUnexpectedEOF)
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

func (d *Decoder) setNextReader(r byteReader, supportBinary bool) error {
	var read func(byteReader) (packet.FrameType, packet.PacketType, int64, error)
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
	if !supportBinary && ft == packet.Binary {
		d.b64Reader = base64.NewDecoder(base64.StdEncoding, &d.limitReader)
	} else {
		d.b64Reader = nil
	}
	return nil
}

func (d *Decoder) sendError(err error) error {
	if d.terminalErr != nil {
		return d.terminalErr
	}
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

func (d *Decoder) textRead(r byteReader) (packet.FrameType, packet.PacketType, int64, error) {
	var l int64
	var err error
	for l == 0 {
		l, err = readTextLen(r)
		if err != nil {
			return 0, 0, 0, err
		}
	}

	ft := packet.String
	b, err := r.ReadByte()
	if err != nil {
		return 0, 0, 0, err
	}
	l--

	if b == 'b' {
		if l < 1 {
			return 0, 0, 0, errInvalidPayload
		}
		ft = packet.Binary
		b, err = r.ReadByte()
		if err != nil {
			return 0, 0, 0, err
		}
		l--
	}

	pt := packet.ByteToPacketType(b, packet.String)
	return ft, pt, l, nil
}

func (d *Decoder) binaryRead(r byteReader) (packet.FrameType, packet.PacketType, int64, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, 0, 0, err
	}
	if b > 1 {
		return 0, 0, 0, errInvalidPayload
	}
	ft := packet.ByteToFrameType(b)

	l, err := readBinaryLen(r)
	if err != nil {
		return 0, 0, 0, err
	}

	if l < 1 {
		return 0, 0, 0, errInvalidPayload
	}
	b, err = r.ReadByte()
	if err != nil {
		return 0, 0, 0, err
	}
	pt := packet.ByteToPacketType(b, ft)
	l--

	return ft, pt, l, nil
}

// NewDecoder binds a payload source. Each acquired reader is released after its last packet or a decoding failure.
func NewDecoder(r Reader) *Decoder { return &Decoder{feeder: r} }
