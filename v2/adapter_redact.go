package socketio

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// redactedHeaders are the header names RedactHandshake drops. A name is compared with
// strings.EqualFold, so every spelling that differs only in case is dropped.
var redactedHeaders = [...]string{"authorization", "cookie", "proxy-authorization"}

var errNotObject = errors.New("not a JSON object")

func redactHandshake(h json.RawMessage) (json.RawMessage, error) {
	if h == nil {
		return nil, nil
	}
	out, err := filterObject(h, func(key string, value []byte) ([]byte, bool, error) {
		switch key {
		case "auth":
			return nil, false, nil
		case "headers":
			if len(value) == 0 || value[0] != '{' {
				return value, true, nil
			}
			headers, err := filterObject(value, func(name string, v []byte) ([]byte, bool, error) {
				return v, !redactedHeader(name), nil
			})
			return headers, err == nil, err
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
		if strings.EqualFold(name, banned) {
			return true
		}
	}
	return false
}

// filterObject rewrites the JSON object src member by member. keep receives the decoded
// key and the raw value of a member and returns the value to write and whether to keep
// the member. Kept members are written with their original key and value bytes, in
// source order, separated by commas without whitespace between members. src must be
// exactly one JSON object; anything else is an error. The result never aliases src.
func filterObject(src []byte, keep func(key string, value []byte) ([]byte, bool, error)) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(src))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errNotObject, err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errNotObject
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
			return nil, errNotObject
		}
		// The bytes between the end of the previous member and the end of the key are
		// whitespace, the separating comma and the key as written.
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
