package parser_test

import (
	"encoding/json"
	"testing"

	"github.com/sshaplygin/go-socket.io/parser"
)

type privateJSONValue struct{ Secret parser.Buffer }

func (privateJSONValue) MarshalJSON() ([]byte, error) { return []byte(`"public"`), nil }

type privateTextValue struct{ Secret parser.Buffer }

func (privateTextValue) MarshalText() ([]byte, error) { return []byte("public"), nil }

// Compare public wire output with the active standard library, including its
// field visibility and method rules. Opaque values must produce no attachments.
func assertJSONPacket(t *testing.T, value any) {
	t.Helper()
	want, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("encoder panicked for valid JSON %s: %v", want, p)
		}
	}()
	w := new(recordedWriter)
	if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Ack}, value); err != nil {
		t.Fatal(err)
	}
	if len(w.frames) != 1 || w.frames[0].String() != "3"+string(want)+"\n" {
		t.Fatalf("frames = %v, want JSON packet %s", w.frames, want)
	}
}

func TestEncoderNilPrivateEmbeddedMarshalers(t *testing.T) {
	t.Run("JSON", func(t *testing.T) {
		assertJSONPacket(t, struct {
			*privateJSONValue `json:"named"`
			MarshalJSON       int `json:"-"`
		}{})
	})
	t.Run("text", func(t *testing.T) {
		assertJSONPacket(t, struct {
			*privateTextValue `json:"named"`
			MarshalText       int `json:"-"`
		}{})
	})
}
