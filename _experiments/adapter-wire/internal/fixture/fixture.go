// Package fixture reads the checked-in Node publications for compatibility tests.
// It is test tooling, not a runtime adapter codec or an untrusted-input parser.
package fixture

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/vmihailenco/msgpack/v5"
)

type File struct {
	FormatVersion int    `json:"formatVersion"`
	Reference     string `json:"reference"`
	Cases         []Case `json:"cases"`
}

type Case struct {
	Name         string        `json:"name"`
	Publications []Publication `json:"publications"`
}

type Publication struct {
	Channel    string          `json:"channel"`
	Encoding   string          `json:"encoding"`
	WireBase64 string          `json:"wireBase64"`
	Value      json.RawMessage `json:"value"`
}

func Load(path string) (File, error) {
	var f File
	body, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(body, &f); err != nil {
		return f, err
	}
	if f.FormatVersion != 1 {
		return f, fmt.Errorf("fixture: unsupported format %d", f.FormatVersion)
	}
	return f, nil
}

func (p Publication) Decode() (any, error) {
	wire, err := base64.StdEncoding.Strict().DecodeString(p.WireBase64)
	if err != nil {
		return nil, err
	}
	var value any
	switch p.Encoding {
	case "json":
		err = json.Unmarshal(wire, &value)
	case "msgpack":
		err = msgpack.Unmarshal(wire, &value)
	default:
		err = fmt.Errorf("fixture: unsupported encoding %q", p.Encoding)
	}
	return value, err
}

// Encode produces a Go counterpart for the Node semantic comparison. Map key
// order and integer widths need not match Node's wire bytes to be interoperable.
func Encode(encoding string, value any) ([]byte, error) {
	switch encoding {
	case "json":
		return json.Marshal(value)
	case "msgpack":
		var out bytes.Buffer
		encoder := msgpack.NewEncoder(&out)
		encoder.SetSortMapKeys(true)
		encoder.UseCompactInts(true)
		if err := encoder.Encode(value); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	default:
		return nil, fmt.Errorf("fixture: unsupported encoding %q", encoding)
	}
}

// Normalize preserves MessagePack binary values in a JSON fixture representation.
// $binary is a fixture marker only and is never emitted on the adapter wire.
func Normalize(value any) any {
	switch v := value.(type) {
	case []byte:
		return map[string]any{"$binary": base64.StdEncoding.EncodeToString(v)}
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = Normalize(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = Normalize(item)
		}
		return out
	default:
		return v
	}
}
