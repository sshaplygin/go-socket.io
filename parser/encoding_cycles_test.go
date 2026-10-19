package parser_test

import (
	"bytes"
	"github.com/sshaplygin/go-socket.io/parser"
	"os"
	"os/exec"
	"runtime/debug"
	"testing"
)

// Covers G8
func TestReviewG8CyclicValueReturnsError(t *testing.T) {
	if os.Getenv("SOCKETIO_AUDIT_CYCLE_CHILD") == "1" {
		debug.SetMaxStack(128 << 10)
		type node struct{ Next *node }
		n := &node{}
		n.Next = n
		if err := parser.NewEncoder(discardFrameWriter{}).Encode(parser.Header{Type: parser.Event}, []any{"event", n}); err == nil {
			t.Error("cycle must return an encoding error")
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestReviewG8CyclicValueReturnsError$")
	cmd.Env = append(os.Environ(), "SOCKETIO_AUDIT_CYCLE_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if bytes.Contains(out, []byte("fatal error: stack overflow")) {
			t.Fatal("cyclic value causes fatal stack overflow in encoder subprocess")
		}
		t.Fatalf("subprocess failed: %v\n%s", err, out)
	}
}
