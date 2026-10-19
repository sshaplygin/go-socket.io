package parser_test

import (
	"github.com/sshaplygin/go-socket.io/parser"
	"io"
	"strings"
	"testing"
)

type queuedFrame struct {
	typ  parser.FrameType
	data string
}
type queuedReader struct{ frames []queuedFrame }

func (r *queuedReader) NextReader() (parser.FrameType, io.ReadCloser, error) {
	if len(r.frames) == 0 {
		return 0, nil, io.EOF
	}
	f := r.frames[0]
	r.frames = r.frames[1:]
	return f.typ, io.NopCloser(strings.NewReader(f.data)), nil
}

// Covers G9
func TestDiscardLastConsumesAttachments(t *testing.T) {
	for _, typ := range []string{"51-/unknown,[\"event\",{\"_placeholder\":true,\"num\":0}]", "61-/unknown,1[{\"_placeholder\":true,\"num\":0}]"} {
		r := &queuedReader{frames: []queuedFrame{{parser.Text, typ}, {parser.Binary, "payload"}, {parser.Text, `2["next"]`}}}
		d := parser.NewDecoder(r)
		var h parser.Header
		var event string
		if err := d.DecodeHeader(&h, &event); err != nil {
			t.Fatal(err)
		}
		if err := d.DiscardLast(); err != nil {
			t.Fatal(err)
		}
		if err := d.DecodeHeader(&h, &event); err != nil {
			t.Fatal(err)
		}
		if event != "next" {
			t.Fatalf("event=%q", event)
		}
	}
}
