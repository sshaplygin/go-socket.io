package socketio

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// handshakeAuthKey and handshakeHeadersKey are the Node key names of the handshake
// object that RedactHandshake looks at; both are matched exactly.
const (
	handshakeAuthKey    = "auth"
	handshakeHeadersKey = "headers"
)

// redactedHeaders are the header names RedactHandshake drops, in lower case. A header
// name is compared with ASCII case folding only, so a name that merely folds to one of
// these under Unicode rules (U+212A KELVIN SIGN for "k") is kept.
var redactedHeaders = [...]string{"authorization", "cookie", "proxy-authorization"}

func redactHandshake(h json.RawMessage) (json.RawMessage, error) {
	if h == nil {
		return nil, nil
	}
	out, err := filterObject(h, func(key string, value []byte) ([]byte, bool, error) {
		switch key {
		case handshakeAuthKey:
			return nil, false, nil
		case handshakeHeadersKey:
			if !isObject(value) {
				return value, true, nil
			}
			headers, err := filterObject(value, func(name string, v []byte) ([]byte, bool, error) {
				return v, !redactedHeader(name), nil
			})
			if err != nil {
				return nil, false, err
			}
			return headers, true, nil
		}
		return value, true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("socketio: redact handshake: %w", err)
	}
	return out, nil
}

func redactedHeader(name string) bool {
	for _, banned := range redactedHeaders {
		if equalFoldASCII(name, banned) {
			return true
		}
	}
	return false
}

// equalFoldASCII reports whether s equals the lower-case ASCII string lower when the
// ASCII letters of s are folded to lower case.
func equalFoldASCII(s, lower string) bool {
	if len(s) != len(lower) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != lower[i] {
			return false
		}
	}
	return true
}

func isObject(raw []byte) bool {
	raw = bytes.TrimLeft(raw, " \t\r\n")
	return len(raw) > 0 && raw[0] == '{'
}

// filterObject rewrites the JSON object src member by member. keep receives the decoded
// key and the raw value of a member and returns the value to write and whether to keep
// the member. Kept members are written with their original key and value bytes, in
// source order, separated by commas and without whitespace between members. src must be
// exactly one JSON object; anything else is an error.
func filterObject(src []byte, keep func(key string, value []byte) ([]byte, bool, error)) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(src))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("not a JSON object: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not a JSON object")
	}
	out := make([]byte, 0, len(src))
	out = append(out, '{')
	first := true
	for dec.More() {
		start := dec.InputOffset()
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, errors.New("not a JSON object")
		}
		rawKey := bytes.TrimLeft(src[start:dec.InputOffset()], " \t\r\n,")
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		newValue, kept, err := keep(key, value)
		if err != nil {
			return nil, err
		}
		if !kept {
			continue
		}
		if !first {
			out = append(out, ',')
		}
		first = false
		out = append(out, rawKey...)
		out = append(out, ':')
		out = append(out, newValue...)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("data after the JSON object")
	}
	return append(out, '}'), nil
}
