package parser_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sshaplygin/go-socket.io/parser"
)

type fuzzRoundTripValue struct {
	Text    string            `json:"text"`
	Number  int64             `json:"number"`
	Enabled bool              `json:"enabled"`
	Tags    []string          `json:"tags,omitempty"`
	Meta    map[string]string `json:"meta,omitempty"`
}

func FuzzRoundTrip(f *testing.F) {
	f.Add("hello", int64(42), true)
	f.Add("<>&\u2028\u2029\x00\xff", int64(-9223372036854775808), false)
	f.Add("", int64(9007199254740993), true)
	f.Fuzz(func(t *testing.T, text string, number int64, enabled bool) {
		value := fuzzRoundTripValue{text, number, enabled, []string{text, "tag"}, map[string]string{text: "value"}}
		// JSON normalizes invalid UTF-8. Compare with that same normalization while
		// retaining exact int64 values in the typed decoder target.
		canonical, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var want fuzzRoundTripValue
		if err := json.Unmarshal(canonical, &want); err != nil {
			t.Fatal(err)
		}
		packet, err := parser.Prepare(parser.Header{Type: parser.Event}, []any{"event", value})
		if err != nil {
			t.Fatal(err)
		}
		wire := new(frames)
		if err := parser.NewEncoder(wire).EncodePrepared(packet); err != nil {
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
		if h.Type != parser.Event || event != "event" {
			t.Fatalf("header=%+v event=%q", h, event)
		}
		values, err := d.DecodeArgs([]reflect.Type{reflect.TypeOf(value)})
		if err != nil {
			t.Fatal(err)
		}
		if len(values) != 1 || !reflect.DeepEqual(values[0].Interface(), want) {
			t.Fatalf("round trip = %v, want %+v", values, want)
		}
	})
}

func FuzzDecodeArgs(f *testing.F) {
	for i, seed := range []string{
		`["event","hello"]`, `["event",9007199254740993]`, `["event",true]`,
		`["event",1.25]`, `["event",[1,2,3]]`, `["event",{"text":"hello"}]`,
		`["event",{"text":"hello","number":7}]`, `["event","\ud800"]`,
		`["event",null]`, `["event",1] trailing`,
	} {
		f.Add(seed, uint8(i))
	}
	targets := []reflect.Type{reflect.TypeOf(""), reflect.TypeOf(int64(0)), reflect.TypeOf(false), reflect.TypeOf(float64(0)), reflect.TypeOf([]int64{}), reflect.TypeOf(map[string]string{}), reflect.TypeOf(fuzzRoundTripValue{})}
	f.Fuzz(func(t *testing.T, payload string, target uint8) {
		wire := new(frames)
		w, err := wire.NextWriter(parser.Text)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("2" + payload)); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		d := parser.NewDecoder(wire)
		defer func() { _ = d.Close() }()
		var h parser.Header
		var event string
		if err := d.DecodeHeader(&h, &event); err != nil {
			return
		}
		_, _ = d.DecodeArgs([]reflect.Type{targets[int(target)%len(targets)]})
	})
}
