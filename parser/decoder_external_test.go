package parser_test

import (
	"errors"
	"github.com/sshaplygin/go-socket.io/parser"
	"io"
	"reflect"
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

type auditReader struct{ closeErr error }

func (r auditReader) NextReader() (parser.FrameType, io.ReadCloser, error) {
	return parser.Text, auditReadCloser{Reader: strings.NewReader(`2["event","payload"]`), closeErr: r.closeErr}, nil
}

type auditReadCloser struct {
	io.Reader
	closeErr error
}

func (r auditReadCloser) Close() error { return r.closeErr }

// Covers G31
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

type framesWithErrors struct {
	frames []io.ReadCloser
	types  []parser.FrameType
}

func (r *framesWithErrors) NextReader() (parser.FrameType, io.ReadCloser, error) {
	if len(r.frames) == 0 {
		return 0, nil, io.EOF
	}
	f, typ := r.frames[0], r.types[0]
	r.frames = r.frames[1:]
	r.types = r.types[1:]
	return typ, f, nil
}

type failedReader struct{ err error }

func (r failedReader) Read([]byte) (int, error) { return 0, r.err }

func TestDecoderAttachmentErrorsAndPrecedence(t *testing.T) {
	primary, secondary := errors.New("read"), errors.New("close")
	for _, test := range []struct {
		name   string
		reader io.Reader
		typ    parser.FrameType
		want   error
	}{
		{"close", strings.NewReader("binary"), parser.Binary, secondary},
		{"read precedes close", failedReader{primary}, parser.Binary, primary},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := &framesWithErrors{frames: []io.ReadCloser{io.NopCloser(strings.NewReader(`51-["event",{"_placeholder":true,"num":0}]`)), auditReadCloser{Reader: test.reader, closeErr: secondary}}, types: []parser.FrameType{parser.Text, test.typ}}
			d := parser.NewDecoder(r)
			var h parser.Header
			var event string
			if err := d.DecodeHeader(&h, &event); err != nil {
				t.Fatal(err)
			}
			if _, err := d.DecodeArgs(nil); !errors.Is(err, test.want) {
				t.Fatalf("got %v; want %v", err, test.want)
			}
		})
	}
}

func TestDiscardClosesEveryAttachmentAfterCloseFailure(t *testing.T) {
	want := errors.New("close")
	r := &framesWithErrors{frames: []io.ReadCloser{
		auditReadCloser{Reader: strings.NewReader(`52-["ignored",{"_placeholder":true,"num":0},{"_placeholder":true,"num":1}]`), closeErr: want},
		io.NopCloser(strings.NewReader("one")), io.NopCloser(strings.NewReader("two")), io.NopCloser(strings.NewReader(`2["next"]`)),
	}, types: []parser.FrameType{parser.Text, parser.Binary, parser.Binary, parser.Text}}
	d := parser.NewDecoder(r)
	var h parser.Header
	var event string
	if err := d.DecodeHeader(&h, &event); err != nil {
		t.Fatal(err)
	}
	if err := d.DiscardLast(); !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
	if err := d.DecodeHeader(&h, &event); err != nil {
		t.Fatal(err)
	}
	if event != "next" {
		t.Fatalf("event=%q", event)
	}
}

func TestDecodeVariadicArguments(t *testing.T) {
	d := parser.NewDecoder(&queuedReader{frames: []queuedFrame{{parser.Text, `2["event","fixed",1,2]`}}})
	var h parser.Header
	var event string
	if err := d.DecodeHeader(&h, &event); err != nil {
		t.Fatal(err)
	}
	values, err := d.DecodeArgsVariadic([]reflect.Type{reflect.TypeOf(""), reflect.TypeOf([]int{})})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 3 || values[0].String() != "fixed" || values[2].Int() != 2 {
		t.Fatalf("values=%v", values)
	}
}

// Covers G20
func TestDecodeVariadicRejectsNilTypes(t *testing.T) {
	for _, types := range [][]reflect.Type{{nil}, {nil, reflect.TypeOf([]int{})}} {
		d := parser.NewDecoder(&queuedReader{frames: []queuedFrame{{parser.Text, `2["event",1,2]`}}})
		var h parser.Header
		var event string
		if err := d.DecodeHeader(&h, &event); err != nil {
			t.Fatal(err)
		}
		if _, err := d.DecodeArgsVariadic(types); err == nil {
			t.Fatal("nil argument type must return an error")
		}
	}
}
