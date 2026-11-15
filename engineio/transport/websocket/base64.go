package websocket

import (
	"encoding/base64"
	"errors"
	"io"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// packetReader reads one packet per websocket message. A binary message starts
// with the packet type byte, a text message with its ASCII digit. A text
// message "b<type><base64 data>" is a binary packet, whatever the b64 query
// says, as engine.io-parser 2.2.1 decodes it for engine.io 3.6.2 and
// engine.io-client: socket.io-client before 1.4 under Node sends attachments
// this way without b64. The payload streams through a strict base64 decoder:
// missing padding or invalid characters fail the read.
type packetReader struct{ frames eioparser.FrameReader }

func (p packetReader) NextReader() (eioparser.FrameType, eioparser.PacketType, io.ReadCloser, error) {
	ft, r, err := p.frames.NextReader()
	if err != nil {
		return 0, 0, nil, err
	}
	var b [1]byte
	if _, err = io.ReadFull(r, b[:]); err != nil {
		_ = r.Close()
		return 0, 0, nil, err
	}
	if ft != eioparser.String || b[0] != 'b' {
		return ft, eioparser.ByteToPacketType(b[0], ft), r, nil
	}
	if _, err = io.ReadFull(r, b[:]); err != nil {
		_ = r.Close()
		return 0, 0, nil, err
	}
	return eioparser.Binary, eioparser.ByteToPacketType(b[0], eioparser.String), &base64Reader{Reader: base64.NewDecoder(base64.StdEncoding, r), frame: r}, nil
}

// base64Reader decodes the rest of a frame. Close drains it, so that invalid
// data after the part a reader consumed still fails, and closes the frame.
type base64Reader struct {
	io.Reader
	frame io.ReadCloser
}

func (r *base64Reader) Close() error {
	_, err := io.Copy(io.Discard, r.Reader)
	return errors.Join(err, r.frame.Close())
}

type base64Writer struct {
	io.WriteCloser
	frame io.WriteCloser
}

func (w base64Writer) Close() error { return errors.Join(w.WriteCloser.Close(), w.frame.Close()) }
