package websocket

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// base64FrameReader translates the text packet prefix into the binary header
// expected by PacketDecoder. The payload streams through a strict base64
// decoder: missing padding or invalid characters fail the read.
type base64FrameReader struct{ wrapper }

func (w base64FrameReader) NextReader() (eioparser.FrameType, io.ReadCloser, error) {
	ft, r, err := w.wrapper.NextReader()
	if err != nil || ft != eioparser.String {
		return ft, r, err
	}
	var first [1]byte
	if _, err = io.ReadFull(r, first[:]); err != nil {
		_ = r.Close()
		return 0, nil, err
	}
	if first[0] != 'b' {
		return ft, &frameBodyReader{Reader: io.MultiReader(bytes.NewReader(first[:]), r), frame: r}, nil
	}
	var typ [1]byte
	if _, err = io.ReadFull(r, typ[:]); err != nil {
		_ = r.Close()
		return 0, nil, err
	}
	typ[0] -= '0'
	return eioparser.Binary, &frameBodyReader{Reader: io.MultiReader(bytes.NewReader(typ[:]), base64.NewDecoder(base64.StdEncoding, r)), frame: r}, nil
}

// frameBodyReader reads a frame body after its first bytes were consumed,
// either passed through or base64-decoded. Close drains it and closes the frame.
type frameBodyReader struct {
	io.Reader
	frame io.ReadCloser
}

func (r *frameBodyReader) Close() error {
	_, err := io.Copy(io.Discard, r.Reader)
	return errors.Join(err, r.frame.Close())
}

type base64Writer struct {
	io.WriteCloser
	frame io.WriteCloser
}

func (w base64Writer) Close() error { return errors.Join(w.WriteCloser.Close(), w.frame.Close()) }
