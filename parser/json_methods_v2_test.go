//go:build go1.27 && goexperiment.jsonv2

package parser_test

import (
	"encoding"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"testing"

	"github.com/sshaplygin/go-socket.io/parser"
)

type appendBinary struct{ Secret parser.Buffer }

func (appendBinary) AppendText(b []byte) ([]byte, error) { return append(b, "public"...), nil }

type streamBinary struct{ Secret parser.Buffer }

func (streamBinary) MarshalJSONTo(e *jsontext.Encoder) error { return e.WriteValue([]byte(`"stream"`)) }

type pointerAppendBinary struct{ Secret parser.Buffer }

func (*pointerAppendBinary) AppendText(b []byte) ([]byte, error) { return append(b, "pointer"...), nil }

type pointerStreamBinary struct{ Secret parser.Buffer }

func (*pointerStreamBinary) MarshalJSONTo(e *jsontext.Encoder) error {
	return e.WriteValue([]byte(`"pointer stream"`))
}

type allJSONMethods struct{ appendBinary }

func (allJSONMethods) MarshalText() ([]byte, error) { return []byte("text"), nil }
func (allJSONMethods) MarshalJSON() ([]byte, error) { return []byte(`"json"`), nil }
func (allJSONMethods) MarshalJSONTo(e *jsontext.Encoder) error {
	return e.WriteValue([]byte(`"stream first"`))
}

type appendByte byte

func (appendByte) AppendText(b []byte) ([]byte, error) { return append(b, "byte"...), nil }

type streamByte byte

func (streamByte) MarshalJSONTo(e *jsontext.Encoder) error {
	return e.WriteValue([]byte(`"stream byte"`))
}

type appendKey struct {
	Name  string
	Calls *int
}

func (k appendKey) AppendText(b []byte) ([]byte, error) {
	if k.Calls != nil {
		*k.Calls++
	}
	return append(b, k.Name...), nil
}

type bothTextKey struct{ appendKey }

func (bothTextKey) MarshalText() ([]byte, error) { return []byte("wrong precedence"), nil }

type appendString string

func (appendString) AppendText(b []byte) ([]byte, error) { return append(b, "appended key"...), nil }

type nilAppendKey struct{}

func (*nilAppendKey) AppendText(b []byte) ([]byte, error) { return append(b, "nil receiver"...), nil }

type failedAppendKey struct{}

var errAppendKey = errors.New("append failed")

func (failedAppendKey) AppendText([]byte) ([]byte, error) { return nil, errAppendKey }

func TestEncoderJSONv2Methods(t *testing.T) {
	secret := parser.Buffer{Data: []byte("secret")}
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"appender", appendBinary{secret}}, {"stream", streamBinary{secret}},
		{"precedence", allJSONMethods{appendBinary{secret}}},
		{"pointer appender", &pointerAppendBinary{secret}}, {"pointer stream", &pointerStreamBinary{secret}},
		{"slice addressability", []pointerAppendBinary{{secret}}},
		{"stream slice addressability", []pointerStreamBinary{{secret}}},
		{"appender byte slice", []appendByte{1, 2}}, {"stream byte slice", []streamByte{1, 2}},
		{"appender byte array", [2]appendByte{1, 2}}, {"stream byte array", [2]streamByte{1, 2}},
		{"static appender nil", struct{ Value encoding.TextAppender }{(*nilAppendKey)(nil)}},
		{"plain appender nil", (*nilAppendKey)(nil)},
		{"static stream nil", struct{ Value jsonv2.MarshalerTo }{(*pointerStreamBinary)(nil)}},
		{"plain stream nil", (*pointerStreamBinary)(nil)},
		{"appender map key", map[appendKey]string{{Name: "key"}: "value"}},
		{"appender map precedence", map[bothTextKey]string{{appendKey{Name: "key"}}: "value"}},
		{"appender string key", map[appendString]string{"key": "value"}},
		{"nil appender key", map[*nilAppendKey]string{nil: "value"}},
	} {
		t.Run(tc.name, func(t *testing.T) { assertJSONPacket(t, tc.value) })
	}
}

func TestEncoderJSONv2UnaddressableMethodsKeepVisibleBuffers(t *testing.T) {
	secret := parser.Buffer{Data: []byte("visible")}
	for _, value := range []any{pointerAppendBinary{secret}, pointerStreamBinary{secret}, map[string]pointerAppendBinary{"key": {secret}}, [1]pointerStreamBinary{{secret}}} {
		w := new(recordedWriter)
		if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Ack}, value); err != nil {
			t.Fatal(err)
		}
		if len(w.frames) != 2 || w.frames[1].String() != "visible" {
			t.Fatalf("unaddressable methods suppressed visible Buffer: %v", w.frames)
		}
	}
}

func TestEncoderAppendMapKeyOnceAndError(t *testing.T) {
	t.Run("once", func(t *testing.T) {
		calls := 0
		w := new(recordedWriter)
		value := map[appendKey]parser.Buffer{{Name: "key", Calls: &calls}: {Data: []byte("visible")}}
		if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Ack}, value); err != nil {
			t.Fatal(err)
		}
		if calls != 1 || len(w.frames) != 2 || w.frames[0].String() != `61-{"key":{"_placeholder":true,"num":0}}`+"\n" || w.frames[1].String() != "visible" {
			t.Fatalf("calls=%d frames=%v", calls, w.frames)
		}
	})
	t.Run("error", func(t *testing.T) {
		w := new(recordedWriter)
		err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Ack}, map[failedAppendKey]parser.Buffer{{}: {Data: []byte("secret")}})
		if !errors.Is(err, errAppendKey) || len(w.frames) != 0 {
			t.Fatalf("err=%v frames=%v", err, w.frames)
		}
	})
}

type privateReadJSON struct{ Secret parser.Buffer }

func (*privateReadJSON) UnmarshalJSON([]byte) error { return nil }

type privateReadText struct{ Secret parser.Buffer }

func (*privateReadText) UnmarshalText([]byte) error { return nil }

type privateReadStream struct{ Secret parser.Buffer }

func (*privateReadStream) UnmarshalJSONFrom(*jsontext.Decoder) error { return nil }

type privateZeroValue struct{ Secret parser.Buffer }

func (privateZeroValue) IsZero() bool { return false }

func TestEncoderJSONv2PrivateMethodFields(t *testing.T) {
	secret := parser.Buffer{Data: []byte("secret")}
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"JSON value", struct {
			privateJSONValue `json:"named"`
			MarshalJSON      int `json:"-"`
		}{privateJSONValue{secret}, 0}},
		{"JSON pointer", struct {
			*privateJSONValue `json:"named"`
			MarshalJSON       int `json:"-"`
		}{&privateJSONValue{secret}, 0}},
		{"text", struct {
			privateTextValue `json:"named"`
			MarshalText      int `json:"-"`
		}{privateTextValue{secret}, 0}},
		{"append", struct {
			appendBinary `json:"named"`
			AppendText   int `json:"-"`
		}{appendBinary{secret}, 0}},
		{"stream", struct {
			streamBinary  `json:"named"`
			MarshalJSONTo int `json:"-"`
		}{streamBinary{secret}, 0}},
		{"read JSON", struct {
			privateReadJSON `json:"named"`
		}{privateReadJSON{secret}}},
		{"read text", struct {
			privateReadText `json:"named"`
		}{privateReadText{secret}}},
		{"read stream", struct {
			privateReadStream `json:"named"`
		}{privateReadStream{secret}}},
		{"zero method", struct {
			privateZeroValue `json:"named,omitzero"`
		}{privateZeroValue{secret}}},
	} {
		t.Run(tc.name, func(t *testing.T) { assertJSONPacket(t, tc.value) })
	}
}

type originalAppendReceiver struct {
	Secret parser.Buffer
	Self   *originalAppendReceiver
	Calls  int
}

func (v *originalAppendReceiver) AppendText(b []byte) ([]byte, error) {
	if v != v.Self {
		return nil, errors.New("receiver was copied")
	}
	v.Calls++
	return append(b, "original"...), nil
}

type originalStreamReceiver struct {
	Secret parser.Buffer
	Self   *originalStreamReceiver
	Calls  int
}

func (v *originalStreamReceiver) MarshalJSONTo(e *jsontext.Encoder) error {
	if v != v.Self {
		return errors.New("receiver was copied")
	}
	v.Calls++
	return e.WriteValue([]byte(`"original"`))
}

func TestEncoderJSONv2CallsOriginalReceiverOnce(t *testing.T) {
	secret := parser.Buffer{Data: []byte("secret")}
	appendValue := &originalAppendReceiver{Secret: secret}
	appendValue.Self = appendValue
	streamValue := &originalStreamReceiver{Secret: secret}
	streamValue.Self = streamValue
	// Each oracle call serializes once with encoding/json and once with parser.
	assertJSONPacket(t, appendValue)
	assertJSONPacket(t, streamValue)
	if appendValue.Calls != 2 || streamValue.Calls != 2 {
		t.Fatalf("method calls: append=%d stream=%d", appendValue.Calls, streamValue.Calls)
	}
	if string(secret.Data) != "secret" {
		t.Fatal("caller-owned buffer changed")
	}
}
