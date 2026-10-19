package parser_test

import (
	"github.com/sshaplygin/go-socket.io/parser"
	"testing"
)

// Covers G12
func TestReviewG12BufferValue(t *testing.T) {
	buffer := parser.Buffer{Data: []byte("hello")}
	for _, value := range []interface{}{buffer, struct{ Binary parser.Buffer }{buffer}, map[string]parser.Buffer{"binary": buffer}} {
		w := new(recordedWriter)
		if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Event}, []any{"event", value}); err != nil {
			t.Fatalf("Buffer value shape %T cannot be emitted: %v", value, err)
		}
		if len(w.frames) != 2 || w.frames[1].String() != "hello" {
			t.Fatalf("shape %T: expected one hello attachment", value)
		}
	}
}
