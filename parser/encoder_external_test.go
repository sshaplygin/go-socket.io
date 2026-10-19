package parser_test

import (
	"bytes"
	"errors"
	"github.com/sshaplygin/go-socket.io/parser"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
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

func TestEncodingContainerCyclesAndSharedChildren(t *testing.T) {
	m := map[string]interface{}{}
	m["self"] = m
	s := make([]interface{}, 1)
	s[0] = s
	for _, value := range []interface{}{m, s} {
		if err := parser.NewEncoder(discardFrameWriter{}).Encode(parser.Header{Type: parser.Event}, []interface{}{"event", value}); err == nil {
			t.Fatal("expected cycle error")
		}
	}
	child := &struct{ Value int }{3}
	if err := parser.NewEncoder(discardFrameWriter{}).Encode(parser.Header{Type: parser.Event}, []interface{}{"event", child, child}); err != nil {
		t.Fatal(err)
	}
}

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

func (discardFrame) Close() error { return nil }

type auditWriter struct{ closeErr error }

func (w auditWriter) NextWriter(parser.FrameType) (io.WriteCloser, error) {
	return auditWriteCloser(w), nil
}

type auditWriteCloser struct{ closeErr error }

func (auditWriteCloser) Write(p []byte) (int, error) { return len(p), nil }

func (w auditWriteCloser) Close() error { return w.closeErr }

// Covers G30
func TestV1AuditEncoderMustReturnFrameCloseError(t *testing.T) {
	want := errors.New("frame flush failed")
	err := parser.NewEncoder(auditWriter{closeErr: want}).Encode(parser.Header{Type: parser.Event}, []any{"event", "payload"})
	if !errors.Is(err, want) {
		t.Fatalf("Encode returned %v; want frame Close error %v", err, want)
	}
}
