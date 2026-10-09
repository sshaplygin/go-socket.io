// oracle is a JSON bridge for the Node cross-implementation check in verify.mjs. It
// reads a JSON array of requests on standard input and writes one result per request
// on standard output. It is a test tool, not a production service.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/sshaplygin/go-socket.io/parser"
)

// wirePacket is the Go-origin packet in the lower-case spelling verify.mjs uses; the
// attachments travel in the request next to it.
type wirePacket struct {
	Type      parser.Type     `json:"type"`
	Namespace string          `json:"namespace"`
	ID        *uint64         `json:"id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

type request struct {
	Packet      *wirePacket   `json:"packet,omitempty"`
	Envelope    string        `json:"envelope"`
	Attachments [][]byte      `json:"attachments"`
	Limits      parser.Limits `json:"limits"`
}

type result struct {
	Envelope    string   `json:"envelope"`
	Attachments [][]byte `json:"attachments"`
	Data        any      `json:"data"`
	Error       string   `json:"error,omitempty"`
}

// normalize turns attachment bytes into the {"$binary": base64} form verify.mjs
// compares with Node buffers.
func normalize(v any) any {
	switch x := v.(type) {
	case []byte:
		return map[string]string{"$binary": base64.StdEncoding.EncodeToString(x)}
	case []any:
		for i, item := range x {
			x[i] = normalize(item)
		}
	case map[string]any:
		for k, item := range x {
			x[k] = normalize(item)
		}
	}
	return v
}

// reconstruct builds the generic tree of a decoded packet with each placeholder
// replaced by its attachment. It is deliberately independent of the parser's own
// placeholder code, so the comparison with Node does not check the parser against
// itself; the parser has already validated every index.
func reconstruct(p parser.Packet) (any, error) {
	if len(p.Data) == 0 {
		return nil, nil
	}
	var tree any
	dec := json.NewDecoder(bytes.NewReader(p.Data))
	dec.UseNumber()
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}
	if len(p.Attachments) == 0 {
		return tree, nil
	}
	return replace(tree, p.Attachments), nil
}

func replace(v any, attachments [][]byte) any {
	switch x := v.(type) {
	case []any:
		for i, item := range x {
			x[i] = replace(item, attachments)
		}
	case map[string]any:
		if x["_placeholder"] == true {
			if n, ok := x["num"].(json.Number); ok {
				if i, err := n.Int64(); err == nil && i >= 0 && int(i) < len(attachments) {
					return attachments[i]
				}
			}
		}
		for k, item := range x {
			x[k] = replace(item, attachments)
		}
	}
	return v
}

func run(r request) result {
	if r.Packet != nil {
		text, atts, err := parser.Encode(parser.Packet{
			Type: r.Packet.Type, Namespace: r.Packet.Namespace, ID: r.Packet.ID,
			Data: r.Packet.Data, Attachments: r.Attachments,
		}, r.Limits)
		if err != nil {
			return result{Error: err.Error()}
		}
		r.Envelope, r.Attachments = string(text), atts
	}
	p, err := parser.Decode([]byte(r.Envelope), r.Attachments, r.Limits)
	if err != nil {
		return result{Error: err.Error()}
	}
	text, atts, err := parser.Encode(p, r.Limits)
	if err != nil {
		return result{Error: err.Error()}
	}
	v, err := reconstruct(p)
	if err != nil {
		return result{Error: err.Error()}
	}
	if atts == nil {
		atts = [][]byte{} // JSON [] rather than null, as Node's encoder reports
	}
	return result{Envelope: string(text), Attachments: atts, Data: normalize(v)}
}

func main() {
	var input []request
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out := make([]result, len(input))
	for i, r := range input {
		out[i] = run(r)
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
