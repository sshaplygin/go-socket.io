package codec_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/vmihailenco/msgpack/v5"
)

// The fixture file is captured from the pinned Node adapter by
// testdata/reference/capture.cjs. The types below read it; they are test tooling.

type fixtureFile struct {
	FormatVersion int           `json:"formatVersion"`
	Reference     string        `json:"reference"`
	Cases         []fixtureCase `json:"cases"`
}

type fixtureCase struct {
	Name         string               `json:"name"`
	Publications []fixturePublication `json:"publications"`
}

type fixturePublication struct {
	Channel    string          `json:"channel"`
	Encoding   string          `json:"encoding"`
	WireBase64 string          `json:"wireBase64"`
	Value      json.RawMessage `json:"value"`
}

func loadFixtures(t testing.TB) fixtureFile {
	t.Helper()
	body, err := os.ReadFile("testdata/publications.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixtureFile
	if err := json.Unmarshal(body, &f); err != nil {
		t.Fatal(err)
	}
	if f.FormatVersion != 1 {
		t.Fatalf("unsupported fixture format %d", f.FormatVersion)
	}
	return f
}

func (p fixturePublication) wire(t testing.TB) []byte {
	t.Helper()
	wire, err := base64.StdEncoding.Strict().DecodeString(p.WireBase64)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// kind selects the codec entry point from the channel name the way a subscriber
// would: request and response channels carry a "-request#" or "-response#" suffix on
// the prefix.
func (p fixturePublication) kind() string {
	switch {
	case strings.Contains(p.Channel, "-request#"):
		return "request"
	case strings.Contains(p.Channel, "-response#"):
		return "response"
	}
	return "broadcast"
}

// decodeGeneric decodes a publication with the MessagePack library or encoding/json
// into generic values, binary values as []byte.
func decodeGeneric(encoding string, wire []byte) (any, error) {
	var value any
	var err error
	switch encoding {
	case "json":
		err = json.Unmarshal(wire, &value)
	case "msgpack":
		err = msgpack.Unmarshal(wire, &value)
	default:
		err = fmt.Errorf("unsupported encoding %q", encoding)
	}
	return value, err
}

// encodeGeneric is the generic Go counterpart of a reference-only publication, which
// the codec does not implement. Map key order and integer widths need not match Node.
func encodeGeneric(encoding string, value any) ([]byte, error) {
	switch encoding {
	case "json":
		return json.Marshal(value)
	case "msgpack":
		var out bytes.Buffer
		enc := msgpack.NewEncoder(&out)
		enc.SetSortMapKeys(true)
		enc.UseCompactInts(true)
		if err := enc.Encode(value); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
	return nil, fmt.Errorf("unsupported encoding %q", encoding)
}

// normalize replaces MessagePack binary values by the fixture marker
// {"$binary": base64}, which is a test-only notation and never on the wire.
func normalize(value any) any {
	switch v := value.(type) {
	case []byte:
		return map[string]any{"$binary": base64.StdEncoding.EncodeToString(v)}
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalize(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = normalize(item)
		}
		return out
	}
	return value
}
