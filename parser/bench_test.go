package parser_test

import (
	"bytes"
	"io"
	"reflect"
	"testing"

	"github.com/sshaplygin/go-socket.io/parser"
)

type benchItem struct {
	User   string            `json:"user"`
	Text   string            `json:"text"`
	Time   int64             `json:"time"`
	Tags   []string          `json:"tags,omitempty"`
	Meta   map[string]string `json:"meta,omitempty"`
	Online bool              `json:"online"`
}

type benchItemNoMap struct {
	User   string   `json:"user"`
	Text   string   `json:"text"`
	Time   int64    `json:"time"`
	Tags   []string `json:"tags,omitempty"`
	Online bool     `json:"online"`
}

type benchScalar struct {
	N int
	S string
	B bool
	F float64
}

func benchPayloads() []struct {
	name  string
	value interface{}
} {
	item := benchItem{
		User:   "alice",
		Text:   "hello world, this is a chat message",
		Time:   1700000000,
		Tags:   []string{"a", "b", "c"},
		Meta:   map[string]string{"k1": "v1", "k2": "v2"},
		Online: true,
	}
	items := make([]benchItem, 100)
	noMaps := make([]benchItemNoMap, 100)
	scalars := make([]benchScalar, 100)
	for i := range items {
		items[i] = item
		noMaps[i] = benchItemNoMap{User: item.User, Text: item.Text, Time: item.Time, Tags: item.Tags, Online: item.Online}
		scalars[i] = benchScalar{N: i, S: "hello", B: true, F: 3.14}
	}
	return []struct {
		name  string
		value interface{}
	}{
		{name: "string", value: "hello"},
		{name: "struct", value: item},
		{name: "structs100", value: items},
		{name: "structsNoMap100", value: noMaps},
		{name: "scalars100", value: scalars},
		{name: "binary", value: &parser.Buffer{Data: make([]byte, 1024)}},
	}
}

// benchPayload returns the payload named name from benchPayloads.
func benchPayload(b *testing.B, name string) interface{} {
	b.Helper()
	for _, tc := range benchPayloads() {
		if tc.name == name {
			return tc.value
		}
	}
	b.Fatalf("unknown benchmark payload %q", name)
	return nil
}

type benchDiscardWriter struct{}

func (benchDiscardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (benchDiscardWriter) Close() error                { return nil }
func (benchDiscardWriter) NextWriter(parser.FrameType) (io.WriteCloser, error) {
	return benchDiscardWriter{}, nil
}

func BenchmarkEncode(b *testing.B) {
	for _, tc := range benchPayloads() {
		b.Run(tc.name, func(b *testing.B) {
			enc := parser.NewEncoder(benchDiscardWriter{})
			args := []interface{}{"event", tc.value}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := enc.Encode(parser.Header{Type: parser.Event}, args); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkEncodePrepared(b *testing.B) {
	b.Run("structs100", func(b *testing.B) {
		packet, err := parser.Prepare(parser.Header{Type: parser.Event}, []interface{}{"event", benchPayload(b, "structs100")})
		if err != nil {
			b.Fatal(err)
		}
		enc := parser.NewEncoder(benchDiscardWriter{})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := enc.EncodePrepared(packet); err != nil {
				b.Fatal(err)
			}
		}
	})
}

type benchFrame struct {
	typ  parser.FrameType
	data *bytes.Buffer
}

type benchFrames struct {
	frames []benchFrame
	next   int
}

type benchBufferCloser struct{ *bytes.Buffer }

func (benchBufferCloser) Close() error { return nil }
func (f *benchFrames) NextWriter(typ parser.FrameType) (io.WriteCloser, error) {
	buf := new(bytes.Buffer)
	f.frames = append(f.frames, benchFrame{typ: typ, data: buf})
	return benchBufferCloser{buf}, nil
}

func (f *benchFrames) NextReader() (parser.FrameType, io.ReadCloser, error) {
	if f.next == len(f.frames) {
		return 0, nil, io.EOF
	}
	frame := f.frames[f.next]
	f.next++
	return frame.typ, io.NopCloser(bytes.NewReader(frame.data.Bytes())), nil
}

func BenchmarkDecode(b *testing.B) {
	for _, tc := range benchPayloads() {
		if tc.name == "structsNoMap100" || tc.name == "scalars100" {
			continue
		}
		b.Run(tc.name, func(b *testing.B) {
			frames := new(benchFrames)
			if err := parser.NewEncoder(frames).Encode(parser.Header{Type: parser.Event}, []interface{}{"event", tc.value}); err != nil {
				b.Fatal(err)
			}
			types := []reflect.Type{reflect.TypeOf(tc.value)}
			dec := parser.NewDecoder(frames)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				frames.next = 0
				var h parser.Header
				var event string
				if err := dec.DecodeHeader(&h, &event); err != nil {
					b.Fatal(err)
				}
				if _, err := dec.DecodeArgs(types); err != nil {
					b.Fatal(err)
				}
				if err := dec.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
