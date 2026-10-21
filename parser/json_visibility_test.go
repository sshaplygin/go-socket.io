package parser_test

import (
	"encoding/json"
	"testing"

	"github.com/sshaplygin/go-socket.io/parser"
)

type hiddenBinary struct {
	Value parser.Buffer `json:"value"`
}
type shadowedBinary struct {
	hiddenBinary
	Value string `json:"value"`
}
type otherBinary struct {
	Value parser.Buffer `json:"value"`
}
type otherUntaggedBinary struct{ Value parser.Buffer }
type ambiguousBinary struct {
	untaggedBinary
	otherUntaggedBinary
}
type untaggedBinary struct{ Value parser.Buffer }
type taggedPublic struct {
	Public string `json:"Value"`
}
type taggedDominance struct {
	untaggedBinary
	taggedPublic
}
type repeatedBinaryA struct{ hiddenBinary }
type repeatedBinaryB struct{ hiddenBinary }
type repeatedBinary struct {
	repeatedBinaryA
	repeatedBinaryB
}
type deepBinary struct{ hiddenBinary }
type nilDominance struct {
	*otherBinary
	deepBinary
}
type hiddenMany struct {
	Value []parser.Buffer `json:"value"`
}
type shadowedMany struct {
	hiddenMany
	Value string `json:"value"`
}
type privateCycle struct {
	Value *privateCycle `json:"value"`
}
type shadowedCycle struct {
	privateCycle
	Value string `json:"value"`
}

type customBinaryJSON struct{ Secret parser.Buffer }

func (customBinaryJSON) MarshalJSON() ([]byte, error) {
	return []byte(`{"value":"public"}`), nil
}

type customBinaryText struct{ Secret parser.Buffer }

func (customBinaryText) MarshalText() ([]byte, error) { return []byte("public"), nil }

type pointerBinaryJSON struct{ Value parser.Buffer }

func (*pointerBinaryJSON) MarshalJSON() ([]byte, error) {
	return []byte(`{"value":"public"}`), nil
}

type customCycle struct{ Next *customCycle }

func (*customCycle) MarshalJSON() ([]byte, error) { return []byte(`"public"`), nil }

func TestEncoderDoesNotTransmitJSONHiddenBuffers(t *testing.T) {
	secret := parser.Buffer{Data: []byte("secret")}
	cycle := &privateCycle{}
	cycle.Value = cycle
	custom := &customCycle{}
	custom.Next = custom
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"shadowed embedded field", shadowedBinary{hiddenBinary{secret}, "public"}},
		{"ambiguous fields", ambiguousBinary{untaggedBinary{secret}, otherUntaggedBinary{secret}}},
		{"tagged field dominates", taggedDominance{untaggedBinary{secret}, taggedPublic{"public"}}},
		{"repeated embedding", repeatedBinary{repeatedBinaryA{hiddenBinary{secret}}, repeatedBinaryB{hiddenBinary{secret}}}},
		{"nil pointer still shadows deeper field", nilDominance{nil, deepBinary{hiddenBinary{secret}}}},
		{"hidden buffers do not consume attachment limit", shadowedMany{hiddenMany{make([]parser.Buffer, 11)}, "public"}},
		{"hidden cycle", shadowedCycle{privateCycle{cycle}, "public"}},
		{"JSON marshaler", customBinaryJSON{secret}},
		{"text marshaler", customBinaryText{secret}},
		{"pointer JSON marshaler", &pointerBinaryJSON{secret}},
		{"addressable slice element marshaler", []pointerBinaryJSON{{secret}}},
		{"marshaled cycle", custom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := []any{"event", tc.value}
			want, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			w := new(recordedWriter)
			if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Event}, payload); err != nil {
				t.Fatalf("valid JSON payload %s was rejected: %v", want, err)
			}
			if len(w.frames) != 1 {
				t.Fatalf("JSON payload %s must not transmit hidden binary data: got %d frames", want, len(w.frames))
			}
			if got := w.frames[0].String(); got != "2"+string(want)+"\n" {
				t.Fatalf("text frame = %q, want JSON event %s", got, want)
			}
		})
	}
}

func TestEncoderPreservesVisibleBuffersAndMarshalerAddressability(t *testing.T) {
	secret := parser.Buffer{Data: []byte("visible")}
	for _, tc := range []struct {
		name  string
		value any
		json  string
	}{
		{"Buffer marshaler", secret, `{"_placeholder":true,"num":0}`},
		{"pointer Buffer marshaler", &secret, `{"_placeholder":true,"num":0}`},
		{"unaddressable value", pointerBinaryJSON{secret}, `{"Value":{"_placeholder":true,"num":0}}`},
		{"unaddressable map value", map[string]pointerBinaryJSON{"item": {secret}}, `{"item":{"Value":{"_placeholder":true,"num":0}}}`},
		{"tagged embedding remains nested", struct {
			hiddenBinary `json:"nested"`
		}{hiddenBinary{secret}}, `{"nested":{"value":{"_placeholder":true,"num":0}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := new(recordedWriter)
			if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Event}, []any{"event", tc.value}); err != nil {
				t.Fatal(err)
			}
			if len(w.frames) != 2 || w.frames[1].String() != "visible" {
				t.Fatalf("visible Buffer lost: got %v", w.frames)
			}
			if got, want := w.frames[0].String(), `51-["event",`+tc.json+"]\n"; got != want {
				t.Fatalf("text frame = %q, want %q", got, want)
			}
		})
	}
}

func TestEncoderDoesNotTransmitUnusedPayloadArguments(t *testing.T) {
	w := new(recordedWriter)
	err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Event}, []any{"event", "public"}, parser.Buffer{Data: []byte("secret")})
	if err != nil {
		if len(w.frames) != 0 {
			t.Fatalf("invalid extra payload was partially written: %v", w.frames)
		}
		return
	}
	if len(w.frames) != 1 || w.frames[0].String() != "2[\"event\",\"public\"]\n" {
		t.Fatalf("unused payload argument transmitted binary data: %v", w.frames)
	}
}
