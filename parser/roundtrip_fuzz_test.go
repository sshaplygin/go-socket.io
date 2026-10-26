package parser_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/sshaplygin/go-socket.io/parser"
)

// fuzzRoundTripValue contains only plain JSON types, so Prepare hands it to
// encoding/json in one call.
type fuzzRoundTripValue struct {
	Text    string            `json:"text"`
	Number  int64             `json:"number"`
	Real    float64           `json:"real"`
	Enabled bool              `json:"enabled"`
	Tags    []string          `json:"tags,omitempty"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// fuzzTraversedValue has an interface field, so Prepare encodes it field by
// field with the package's own string, number and map writers.
type fuzzTraversedValue struct {
	Any     any            `json:"any"`
	Text    string         `json:"text"`
	Number  int64          `json:"number"`
	Real    float64        `json:"real"`
	Enabled bool           `json:"enabled"`
	Meta    map[string]any `json:"meta,omitempty"`
}

// fuzzNumbers packs an integer and a float argument. The fuzzer mutates byte
// slices with bit flips and overwrites, but changes integer and float arguments
// only by small steps, so bytes reach every exponent and sign.
func fuzzNumbers(number int64, fraction float64) []byte {
	return binary.LittleEndian.AppendUint64(binary.LittleEndian.AppendUint64(nil, uint64(number)), math.Float64bits(fraction))
}

func FuzzRoundTrip(f *testing.F) {
	f.Add("event", "hello", "key", fuzzNumbers(42, 1.5), true)
	f.Add("<>&", "<>&\u2028\u2029\x00\x1f\"\\\xff", "\xfe\b\f\n\r\t", fuzzNumbers(math.MinInt64, math.Copysign(0, -1)), false)
	f.Add("", "", "", fuzzNumbers(9007199254740993, 1e-7), true)
	f.Add("\xed\xa0\x80", "λ", "é", fuzzNumbers(-1, 1e21), false)
	f.Fuzz(func(t *testing.T, name, text, key string, numbers []byte, enabled bool) {
		numbers = append(numbers, make([]byte, 16)...)
		number := int64(binary.LittleEndian.Uint64(numbers))
		fraction := math.Float64frombits(binary.LittleEndian.Uint64(numbers[8:]))
		args := []any{
			name, text, number, fraction, enabled,
			map[string]any{key: text, text: number},
			fuzzTraversedValue{[]any{text, number, fraction}, text, number, fraction, enabled, map[string]any{key: []any{text, enabled}}},
			fuzzRoundTripValue{text, number, fraction, enabled, []string{text, key}, map[string]string{key: text}},
		}
		types := []reflect.Type{
			reflect.TypeOf(""), reflect.TypeOf(int64(0)), reflect.TypeOf(float64(0)), reflect.TypeOf(false),
			reflect.TypeOf(map[string]any{}), reflect.TypeOf(fuzzTraversedValue{}), reflect.TypeOf(fuzzRoundTripValue{}),
		}
		canonical, standardErr := json.Marshal(args)
		packet, err := parser.Prepare(parser.Header{Type: parser.Event}, args)
		if (err == nil) != (standardErr == nil) {
			t.Fatalf("Prepare error = %v, encoding/json error = %v", err, standardErr)
		}
		if err != nil {
			return
		}
		wire := new(frames)
		if err := parser.NewEncoder(wire).EncodePrepared(packet); err != nil {
			t.Fatal(err)
		}
		if len(wire.all) != 1 || wire.all[0].typ != parser.Text || string(wire.all[0].data) != "2"+string(canonical)+"\n" {
			t.Fatalf("frames = %q, want one text frame 2%s", wire.all, canonical)
		}

		// The expected values are encoding/json's own decoding of its encoding,
		// which normalizes invalid UTF-8 and float64 values the same way.
		var raw []json.RawMessage
		if err := json.Unmarshal(canonical, &raw); err != nil {
			t.Fatal(err)
		}
		var wantName string
		if err := json.Unmarshal(raw[0], &wantName); err != nil {
			t.Fatal(err)
		}
		d := parser.NewDecoder(wire)
		defer func() {
			if err := d.Close(); err != nil {
				t.Error(err)
			}
		}()
		var h parser.Header
		var event string
		if err := d.DecodeHeader(&h, &event); err != nil {
			t.Fatal(err)
		}
		if h != (parser.Header{Type: parser.Event}) || event != wantName {
			t.Fatalf("header = %+v, event = %q; want event %q", h, event, wantName)
		}
		values, err := d.DecodeArgs(types)
		if err != nil {
			t.Fatal(err)
		}
		for i, typ := range types {
			want := reflect.New(typ)
			if err := json.Unmarshal(raw[i+1], want.Interface()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(values[i].Interface(), want.Elem().Interface()) {
				t.Fatalf("argument %d = %#v, want %#v", i, values[i].Interface(), want.Elem().Interface())
			}
		}
	})
}

// FuzzDecodeArgs decodes the first argument of an arbitrary event packet into one
// of a fixed set of types. When the payload is a JSON array, the accepted header,
// event name, argument value and error must match encoding/json on that array.
func FuzzDecodeArgs(f *testing.F) {
	for i, seed := range []string{
		`["event","hello"]`, `["event",9007199254740993]`, `["event",true]`,
		`["event",1.25]`, `["event",[1,2,3]]`, `["event",{"text":"hello"}]`,
		`["event",{"text":"hello","number":7}]`, `["event","\ud800"]`,
		`["event",null]`, `["event",1] trailing`, ` ["a\"]",{"k":[","]},2] `,
		`[1e400,"x"]`, `[{},1]`, `[]`, `["only"]`, `/admin,7["event",{"any":"x"}]`,
	} {
		f.Add(seed, uint8(i))
	}
	targets := []reflect.Type{
		reflect.TypeOf(""), reflect.TypeOf(int64(0)), reflect.TypeOf(false), reflect.TypeOf(float64(0)),
		reflect.TypeOf([]int64{}), reflect.TypeOf(map[string]string{}), reflect.TypeOf(fuzzRoundTripValue{}),
		reflect.TypeOf(fuzzTraversedValue{}), reflect.TypeOf((*string)(nil)), reflect.TypeOf((*any)(nil)).Elem(),
	}
	f.Fuzz(func(t *testing.T, payload string, target uint8) {
		typ := targets[int(target)%len(targets)]
		wire := &frames{all: []wireFrame{{parser.Text, []byte("2" + payload)}}}
		d := parser.NewDecoder(wire)
		defer func() { _ = d.Close() }()
		var h parser.Header
		var event string
		headerErr := d.DecodeHeader(&h, &event)

		// The decoder trims the payload and validates it as a whole. A payload
		// that starts with '[' carries no namespace or ID.
		trimmed := bytes.TrimSpace([]byte(payload))
		if len(trimmed) == 0 || trimmed[0] != '[' {
			if headerErr == nil {
				_, _ = d.DecodeArgs([]reflect.Type{typ})
			}
			return
		}
		var array []json.RawMessage
		if json.Unmarshal(trimmed, &array) != nil || len(array) == 0 || !isEventName(array[0]) {
			if headerErr == nil {
				t.Fatalf("DecodeHeader accepted %q", payload)
			}
			return
		}
		if headerErr != nil {
			t.Fatalf("DecodeHeader(%q) = %v", payload, headerErr)
		}
		if h != (parser.Header{Type: parser.Event}) {
			t.Fatalf("header = %+v", h)
		}
		if array[0][0] == '"' {
			var name string
			if err := json.Unmarshal(array[0], &name); err != nil {
				t.Fatal(err)
			}
			if event != name {
				t.Fatalf("event = %q, want %q", event, name)
			}
		}
		want := reflect.New(typ)
		var wantErr error
		if len(array) > 1 {
			wantErr = json.Unmarshal(array[1], want.Interface())
		}
		values, err := d.DecodeArgs([]reflect.Type{typ})
		if (err == nil) != (wantErr == nil) {
			t.Fatalf("DecodeArgs(%q, %v) error = %v, encoding/json error = %v", payload, typ, err, wantErr)
		}
		if err == nil && !reflect.DeepEqual(values[0].Interface(), want.Elem().Interface()) {
			t.Fatalf("DecodeArgs(%q, %v) = %#v, want %#v", payload, typ, values[0].Interface(), want.Elem().Interface())
		}
	})
}

// isEventName reports whether a JSON value is a valid event name: a string or a
// number, which the decoder formats as JavaScript does.
func isEventName(raw json.RawMessage) bool {
	return raw[0] == '"' || raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9')
}
