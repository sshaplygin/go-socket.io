package parser_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/sshaplygin/go-socket.io/parser"
)

type namedIntPointer *int

type jsonByte byte

func (v jsonByte) MarshalJSON() ([]byte, error) { return json.Marshal(fmt.Sprintf("byte:%d", v)) }

type jsonBool bool

func (jsonBool) MarshalJSON() ([]byte, error) { return []byte(`{"custom":true}`), nil }

type textString string

func (textString) MarshalText() ([]byte, error) { return []byte("text"), nil }

type nilJSON struct{}

func (*nilJSON) MarshalJSON() ([]byte, error) { return []byte(`"nil receiver"`), nil }

type textKey struct {
	Name  string
	Calls *int
}

func (v textKey) MarshalText() ([]byte, error) {
	*v.Calls++
	return []byte(v.Name), nil
}

type failedTextKey struct{}

var errTextKey = errors.New("text key failed")

func (failedTextKey) MarshalText() ([]byte, error) { return nil, errTextKey }

func TestEncoderMatchesJSONValueSemantics(t *testing.T) {
	number := 42
	// The string option is ignored for interface fields. Construct this valid
	// runtime input dynamically because staticcheck rejects that tag in source.
	interfaceTag := reflect.New(reflect.StructOf([]reflect.StructField{{
		Name: "Value", Type: reflect.TypeOf((*any)(nil)).Elem(), Tag: `json:",string"`,
	}})).Elem()
	interfaceTag.Field(0).Set(reflect.ValueOf(number))
	for _, value := range []any{
		struct {
			Bool   bool    `json:",string"`
			Number *int    `json:",string"`
			Nil    *int    `json:",string"`
			Float  float64 `json:",string"`
			String string  `json:",string"`
		}{true, &number, nil, 1.25, "line\n\"quoted\""},
		struct {
			Custom jsonBool `json:",string"`
		}{true},
		struct {
			Named namedIntPointer `json:",string"`
		}{namedIntPointer(&number)},
		interfaceTag.Interface(),
		[]byte{0, 255}, []byte(nil), []byte{}, []jsonByte{0, 255}, [2]byte{0, 255},
		map[int]string{2: "two", 10: "ten"}, map[textString]string{"key": "value"}, textString("value"),
		struct{ Custom json.Marshaler }{(*nilJSON)(nil)},
		struct{ Custom *nilJSON }{},
		[1]pointerBinaryJSON{}, &[1]pointerBinaryJSON{},
	} {
		t.Run(fmt.Sprintf("%T", value), func(t *testing.T) {
			payload := []any{"event", value}
			want, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			w := new(recordedWriter)
			if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Event}, payload); err != nil {
				t.Fatal(err)
			}
			// The unaddressable array retains its visible, empty Buffer.
			if _, binary := value.([1]pointerBinaryJSON); binary {
				if len(w.frames) != 2 || w.frames[0].String() != "51-[\"event\",[{\"Value\":{\"_placeholder\":true,\"num\":0}}]]\n" {
					t.Fatalf("array addressability changed: %v", w.frames)
				}
				return
			}
			if len(w.frames) != 1 || w.frames[0].String() != "2"+string(want)+"\n" {
				t.Fatalf("frames = %v, want JSON event %s", w.frames, want)
			}
		})
	}
}

func TestEncoderResolvesTextMapKeysOnce(t *testing.T) {
	calls := 0
	value := map[textKey]parser.Buffer{
		{"z", &calls}: {Data: []byte("last")},
		{"a", &calls}: {Data: []byte("first")},
	}
	f := new(frames)
	if err := parser.NewEncoder(f).Encode(parser.Header{Type: parser.Event}, []any{"event", value}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("MarshalText called %d times for two map keys", calls)
	}
	d := parser.NewDecoder(f)
	var h parser.Header
	var event string
	if err := d.DecodeHeader(&h, &event); err != nil {
		t.Fatal(err)
	}
	args, err := d.DecodeArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	got := args[0].Interface().(map[string]any)
	if string(got["a"].(*parser.Buffer).Data) != "first" || string(got["z"].(*parser.Buffer).Data) != "last" {
		t.Fatalf("map keys no longer identify their binary values: %v", got)
	}
}

func TestEncoderReturnsTextMapKeyErrorBeforeWriting(t *testing.T) {
	w := new(recordedWriter)
	err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Event}, []any{"event", map[failedTextKey]parser.Buffer{{}: {Data: []byte("secret")}}})
	if err == nil || len(w.frames) != 0 {
		t.Fatalf("map key error must reject the packet before writing: err=%v frames=%v", err, w.frames)
	}
}

type pointerTextKey struct{ Name string }

func (v *pointerTextKey) MarshalText() ([]byte, error) { return []byte(v.Name), nil }

func TestEncoderPreservesNilTextMapKey(t *testing.T) {
	value := map[*pointerTextKey]string{nil: "empty", {"named"}: "value"}
	want, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	w := new(recordedWriter)
	if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Ack}, value); err != nil {
		t.Fatal(err)
	}
	if len(w.frames) != 1 || w.frames[0].String() != "3"+string(want)+"\n" {
		t.Fatalf("nil map key changed: %v, want %s", w.frames, want)
	}
}
