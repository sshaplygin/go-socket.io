package parser_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

func TestEncodeOmitEmptyMatchesEncodingJSON(t *testing.T) {
	number := 7
	var nilInterface any
	var zeroInterface any = 0
	cases := []struct {
		name  string
		value reflect.Value
	}{
		{"bool", reflect.ValueOf(false)}, {"bool-set", reflect.ValueOf(true)},
		{"int", reflect.ValueOf(int(0))}, {"int8", reflect.ValueOf(int8(0))}, {"int16", reflect.ValueOf(int16(0))}, {"int32", reflect.ValueOf(int32(0))}, {"int64", reflect.ValueOf(int64(0))},
		{"uint", reflect.ValueOf(uint(0))}, {"uint8", reflect.ValueOf(uint8(0))}, {"uint16", reflect.ValueOf(uint16(0))}, {"uint32", reflect.ValueOf(uint32(0))}, {"uint64", reflect.ValueOf(uint64(0))}, {"uintptr", reflect.ValueOf(uintptr(0))},
		{"float32", reflect.ValueOf(float32(0))}, {"float64", reflect.ValueOf(float64(0))}, {"number-set", reflect.ValueOf(number)},
		{"string", reflect.ValueOf("")}, {"string-set", reflect.ValueOf("value")},
		{"slice-nil", reflect.ValueOf([]string(nil))}, {"slice-empty", reflect.ValueOf([]string{})}, {"slice-set", reflect.ValueOf([]string{"value"})},
		{"map-nil", reflect.ValueOf(map[string]int(nil))}, {"map-empty", reflect.ValueOf(map[string]int{})}, {"map-set", reflect.ValueOf(map[string]int{"n": 1})},
		{"array-empty", reflect.ValueOf([0]int{})}, {"array-zero", reflect.ValueOf([1]int{})},
		{"pointer-nil", reflect.ValueOf((*int)(nil))}, {"pointer-set", reflect.ValueOf(&number)},
		{"interface-nil", reflect.ValueOf(&nilInterface).Elem()}, {"interface-zero", reflect.ValueOf(&zeroInterface).Elem()},
		{"struct-zero", reflect.ValueOf(struct{ N int }{})},
		{"buffer", reflect.ValueOf(parser.Buffer{})}, {"buffer-pointer-nil", reflect.ValueOf((*parser.Buffer)(nil))}, {"buffer-pointer-set", reflect.ValueOf(&parser.Buffer{Data: []byte("payload")})},
	}
	bufferType := reflect.TypeOf(parser.Buffer{})
	rawType := reflect.TypeOf(json.RawMessage{})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A visible Buffer keeps the parent on the attachment-aware traversal even
			// when Value alone could use encoding/json's fast path.
			typ := reflect.StructOf([]reflect.StructField{
				{Name: "Value", Type: tc.value.Type(), Tag: `json:"value,omitempty"`},
				{Name: "Binary", Type: bufferType, Tag: `json:"binary,omitempty"`},
			})
			original := reflect.New(typ).Elem()
			original.Field(0).Set(tc.value)
			mirrorValue := tc.value
			attachments := 1
			if tc.value.Type() == bufferType {
				mirrorValue = reflect.ValueOf(json.RawMessage(`{"_placeholder":true,"num":0}`))
				attachments++
			}
			if tc.value.Type() == reflect.PointerTo(bufferType) {
				var raw *json.RawMessage
				if !tc.value.IsNil() {
					placeholder := json.RawMessage(`{"_placeholder":true,"num":0}`)
					raw = &placeholder
					attachments++
				}
				mirrorValue = reflect.ValueOf(raw)
			}
			mirrorType := reflect.StructOf([]reflect.StructField{
				{Name: "Value", Type: mirrorValue.Type(), Tag: `json:"value,omitempty"`},
				{Name: "Binary", Type: rawType, Tag: `json:"binary,omitempty"`},
			})
			mirror := reflect.New(mirrorType).Elem()
			mirror.Field(0).Set(mirrorValue)
			mirror.Field(1).Set(reflect.ValueOf(json.RawMessage(fmt.Sprintf(`{"_placeholder":true,"num":%d}`, attachments-1))))
			want, err := json.Marshal([]any{"event", mirror.Interface()})
			if err != nil {
				t.Fatal(err)
			}
			w := new(recordedWriter)
			if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Event}, []any{"event", original.Interface()}); err != nil {
				t.Fatal(err)
			}
			if len(w.frames) != attachments+1 {
				t.Fatalf("attachments = %d, want %d", len(w.frames)-1, attachments)
			}
			if got := w.frames[0].String(); got != fmt.Sprintf("5%d-%s\n", attachments, want) {
				t.Fatalf("packet = %s, want %s", got, want)
			}
		})
	}
}

func TestEncodePlainTypesMatchEncodingJSON(t *testing.T) {
	type embedded struct {
		Name string `json:"name"`
	}
	type fields struct {
		embedded
		Hidden  string   `json:"-"`
		Number  int      `json:"number,string"`
		Empty   []string `json:"empty,omitempty"`
		private string
	}
	for _, value := range []any{
		[][]int{{1, 2}, nil, {}}, map[string][]float64{"x": {1.5, 2.5}}, [2][2]string{{"a", "b"}, {"<", ">"}},
		fields{embedded: embedded{"visible"}, Hidden: "hidden", Number: 42, private: "private"},
		[]map[int]string{{2: "two", 10: "ten"}}, []byte{0, 255},
	} {
		t.Run(fmt.Sprintf("%T", value), func(t *testing.T) {
			want, err := json.Marshal([]any{"event", value})
			if err != nil {
				t.Fatal(err)
			}
			w := new(recordedWriter)
			if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Event}, []any{"event", value}); err != nil {
				t.Fatal(err)
			}
			if len(w.frames) != 1 || w.frames[0].String() != "2"+string(want)+"\n" {
				t.Fatalf("frames=%v, want %s", w.frames, want)
			}
		})
	}
}

func TestEncodeScalarsMatchEncodingJSON(t *testing.T) {
	for _, value := range []any{
		"\x00\x01\b\f\n\r\t\"\\<>&\u2028\u2029\xffλ", true, false, int64(-9223372036854775808), uint64(18446744073709551615),
		json.Number("1e100"), json.Number("9007199254740993"), json.Number(""),
		float32(1e-7), float32(1e20), float32(1e21), float64(1e-7), float64(1e-6), float64(1e20), float64(1e21),
	} {
		t.Run(fmt.Sprintf("%T/%v", value, value), func(t *testing.T) {
			payload := []any{"event", value}
			want, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			w := new(recordedWriter)
			if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Event}, payload); err != nil {
				t.Fatal(err)
			}
			if len(w.frames) != 1 || w.frames[0].String() != "2"+string(want)+"\n" {
				t.Fatalf("frames=%v, want %s", w.frames, want)
			}
		})
	}
}

func TestEncodeNonFiniteScalarsMatchEncodingJSON(t *testing.T) {
	for _, value := range []any{math.NaN(), math.Inf(1), math.Inf(-1), float32(math.Inf(1))} {
		_, standardErr := json.Marshal(value)
		_, err := parser.Prepare(parser.Header{Type: parser.Event}, []any{"event", value})
		var got, want *json.UnsupportedValueError
		if !errors.As(err, &got) || !errors.As(standardErr, &want) || got.Str != want.Str {
			t.Fatalf("value %v: error = %v, want %v", value, err, standardErr)
		}
	}
}
