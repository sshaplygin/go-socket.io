// oracle is a JSON bridge for the optional Node cross-implementation checks.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	wire "github.com/sshaplygin/go-socket.io/experiments/sio5-codec"
	"os"
)

type request struct {
	Packet      *wire.Packet `json:"packet,omitempty"`
	Envelope    string       `json:"envelope"`
	Attachments [][]byte     `json:"attachments"`
	Limits      wire.Limits  `json:"limits"`
}
type result struct {
	Envelope    string   `json:"envelope"`
	Attachments [][]byte `json:"attachments"`
	Data        any      `json:"data"`
	Error       string   `json:"error,omitempty"`
}

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
func run(r request) result {
	if r.Packet != nil {
		body, err := wire.Encode(*r.Packet, r.Limits)
		if err != nil {
			return result{Error: err.Error()}
		}
		r.Envelope = string(body)
	}
	g, err := wire.DecodeGroup([]byte(r.Envelope), r.Attachments, r.Limits)
	if err != nil {
		return result{Error: err.Error()}
	}
	body, b, err := wire.EncodeGroup(g, r.Limits)
	if err != nil {
		return result{Error: err.Error()}
	}
	v, err := wire.Reconstruct(g, r.Limits)
	if err != nil {
		return result{Error: err.Error()}
	}
	return result{Envelope: string(body), Attachments: b, Data: normalize(v)}
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
