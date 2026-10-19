package parser_test

import (
	"bytes"
	"errors"
	"github.com/sshaplygin/go-socket.io/parser"
	"io"
	"strings"
	"testing"
)

type recordedWriter struct{ frames []*bytes.Buffer }

func (w *recordedWriter) NextWriter(parser.FrameType) (io.WriteCloser, error) {
	b := new(bytes.Buffer)
	w.frames = append(w.frames, b)
	return recordedFrame{b}, nil
}

type recordedFrame struct{ *bytes.Buffer }

func (recordedFrame) Close() error { return nil }

func TestPreparedPacketFreezesJSONAndBinary(t *testing.T) {
	value := map[string]interface{}{"text": "before", "binary": parser.Buffer{Data: []byte("before")}}
	p, err := parser.Prepare(parser.Header{Type: parser.Event}, []interface{}{"event", value})
	if err != nil {
		t.Fatal(err)
	}
	value["text"] = "after"
	value["binary"].(parser.Buffer).Data[0] = 'x'
	w := new(recordedWriter)
	h := parser.Header{Type: parser.Event, Namespace: "/chat", NeedAck: true, ID: 42}
	if err := parser.NewEncoder(w).EncodePrepared(p.WithHeader(h)); err != nil {
		t.Fatal(err)
	}
	if got := w.frames[0].String(); !strings.HasPrefix(got, `51-/chat,42["event",`) || !strings.Contains(got, `"text":"before"`) {
		t.Fatalf("unexpected frozen text: %s", got)
	}
	if got := w.frames[1].String(); got != "before" {
		t.Fatalf("attachment=%q", got)
	}
}

type failingWriteFrame struct{ primary, closeErr error }

func (w failingWriteFrame) Write([]byte) (int, error) { return 0, w.primary }

func (w failingWriteFrame) Close() error { return w.closeErr }

type failingFrameWriter struct{ frame failingWriteFrame }

func (w failingFrameWriter) NextWriter(parser.FrameType) (io.WriteCloser, error) { return w.frame, nil }

func TestEncoderPreservesWriteErrorOverCloseError(t *testing.T) {
	primary := errors.New("write")
	secondary := errors.New("close")
	err := parser.NewEncoder(failingFrameWriter{failingWriteFrame{primary, secondary}}).Encode(parser.Header{Type: parser.Event}, []interface{}{"event"})
	if !errors.Is(err, primary) {
		t.Fatalf("got %v", err)
	}
}

func FuzzDecoderDiscardPacket(f *testing.F) {
	for _, seed := range []string{`2["event",1]`, `51-/other,["event",{"_placeholder":true,"num":0}]`, `61-2[{"_placeholder":true,"num":0}]`, `51-["event",{"_placeholder":true,"num":1}]`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, packet string) {
		d := parser.NewDecoder(&queuedReader{frames: []queuedFrame{{parser.Text, packet}, {parser.Binary, "binary"}}})
		var h parser.Header
		var event string
		if err := d.DecodeHeader(&h, &event); err == nil {
			_, _ = d.DecodeArgs(nil)
		}
		_ = d.Close()
	})
}
