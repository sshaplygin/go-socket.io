package parser_test

import (
	"github.com/sshaplygin/go-socket.io/parser"
	"testing"
)

// Covers G12
func TestReviewG12BufferValue(t *testing.T) {
	err := parser.NewEncoder(discardFrameWriter{}).Encode(parser.Header{Type: parser.Event}, []any{"event", parser.Buffer{Data: []byte("hello")}})
	if err != nil {
		t.Fatalf("Buffer by value cannot be emitted: %v", err)
	}
}
