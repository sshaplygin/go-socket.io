package parser_test

import (
	"errors"
	"github.com/sshaplygin/go-socket.io/parser"
	"io"
	"strings"
	"testing"
)

type auditWriter struct{ closeErr error }

func (w auditWriter) NextWriter(parser.FrameType) (io.WriteCloser, error) {
	return auditWriteCloser{closeErr: w.closeErr}, nil
}

type auditWriteCloser struct{ closeErr error }

func (auditWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (w auditWriteCloser) Close() error              { return w.closeErr }

func TestV1AuditEncoderMustReturnFrameCloseError(t *testing.T) {
	want := errors.New("frame flush failed")
	err := parser.NewEncoder(auditWriter{closeErr: want}).Encode(parser.Header{Type: parser.Event}, []any{"event", "payload"})
	if !errors.Is(err, want) {
		t.Fatalf("Encode returned %v; want frame Close error %v", err, want)
	}
}

type auditReader struct{ closeErr error }

func (r auditReader) NextReader() (parser.FrameType, io.ReadCloser, error) {
	return parser.Text, auditReadCloser{Reader: strings.NewReader(`2["event","payload"]`), closeErr: r.closeErr}, nil
}

type auditReadCloser struct {
	io.Reader
	closeErr error
}

func (r auditReadCloser) Close() error { return r.closeErr }

func TestV1AuditDecoderMustReturnFrameCloseError(t *testing.T) {
	want := errors.New("frame release failed")
	d := parser.NewDecoder(auditReader{closeErr: want})
	var h parser.Header
	var event string
	if err := d.DecodeHeader(&h, &event); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DecodeArgs(nil); !errors.Is(err, want) {
		t.Fatalf("DecodeArgs returned %v; want frame Close error %v", err, want)
	}
}
