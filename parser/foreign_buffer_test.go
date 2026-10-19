package parser_test

import (
	"bytes"
	"github.com/sshaplygin/go-socket.io/parser"
	"io"
	"testing"
)

// Covers G7
func TestV1AuditForeignBufferMustNotPanic(t *testing.T) {
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("encoding an ordinary *bytes.Buffer panicked: %v", p)
		}
	}()
	err := parser.NewEncoder(discardFrameWriter{}).Encode(parser.Header{Type: parser.Event}, []any{"event", bytes.NewBufferString("hello")})
	if err != nil {
		t.Fatalf("ordinary JSON value rejected: %v", err)
	}
}

type discardFrameWriter struct{}

func (discardFrameWriter) NextWriter(parser.FrameType) (io.WriteCloser, error) {
	return discardFrame{}, nil
}

type discardFrame struct{}

func (discardFrame) Write(p []byte) (int, error) { return len(p), nil }
func (discardFrame) Close() error                { return nil }
