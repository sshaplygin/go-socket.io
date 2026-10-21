package parser_test

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"testing"

	"github.com/sshaplygin/go-socket.io/parser"
)

type wireFrame struct {
	typ  parser.FrameType
	data []byte
}
type frames struct{ all []wireFrame }
type frameWriter struct {
	bytes.Buffer
	target *frames
	typ    parser.FrameType
}

func (w *frameWriter) Close() error {
	w.target.all = append(w.target.all, wireFrame{w.typ, bytes.Clone(w.Bytes())})
	return nil
}
func (f *frames) NextWriter(t parser.FrameType) (io.WriteCloser, error) {
	return &frameWriter{target: f, typ: t}, nil
}
func (f *frames) NextReader() (parser.FrameType, io.ReadCloser, error) {
	if len(f.all) == 0 {
		return 0, nil, io.EOF
	}
	v := f.all[0]
	f.all = f.all[1:]
	return v.typ, io.NopCloser(bytes.NewReader(v.data)), nil
}
func TestAttachmentRoundTrip(t *testing.T) {
	for _, n := range []int{10, 11} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			f := new(frames)
			bufs := make([]parser.Buffer, n)
			for i := range bufs {
				bufs[i].Data = []byte{byte(i)}
			}
			err := parser.NewEncoder(f).Encode(parser.Header{Type: parser.Event}, []interface{}{"files", bufs})
			if n > 10 {
				if err == nil || len(f.all) != 0 {
					t.Fatalf("oversized packet must fail before writing, err=%v frames=%d", err, len(f.all))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			decoder := parser.NewDecoder(f)
			var h parser.Header
			var event string
			if err := decoder.DecodeHeader(&h, &event); err != nil {
				t.Fatalf("encoder accepted %d attachments; decoder rejected its own packet: %v", n, err)
			}
			vals, err := decoder.DecodeArgs([]reflect.Type{reflect.TypeOf(bufs)})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(vals[0].Interface(), bufs) {
				t.Fatal("round trip mismatch")
			}
		})
	}
}

type publicPayload struct {
	Value  string
	parent *publicPayload
}

func TestIgnoredPrivateField(t *testing.T) {
	p := &publicPayload{Value: "valid"}
	p.parent = p
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("encoding/json: %s", b)
	_, err = parser.Prepare(parser.Header{Type: parser.Event}, []interface{}{"data", p})
	if err != nil {
		t.Fatalf("JSON-ignored private field prevents Emit: %v", err)
	}
}
