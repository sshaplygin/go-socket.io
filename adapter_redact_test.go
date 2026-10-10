package socketio_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	socketio "github.com/sshaplygin/go-socket.io"
)

// nodeHandshake is the Handshake of a Node peer as fetchSockets sends it: it carries
// the auth object, the three credential headers in mixed case, and the fields the
// contract passes through even though they may hold credentials (x-api-key, set-cookie,
// a token in query).
const nodeHandshake = `{"headers":{"Authorization":"Bearer secret","COOKIE":"sid=1","Proxy-Authorization":"Basic xyz","x-api-key":"k-123","Set-Cookie":"a=b","host":"example.test"},"time":"Mon Jan 01 2024","address":"10.0.0.1","xdomain":false,"secure":true,"issued":1700000000000,"url":"/socket.io/?EIO=4&token=url-secret","query":{"EIO":"4","token":"query-secret"},"auth":{"token":"auth-secret"}}`

func decodeObject(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, raw)
	}
	return m
}

// TestRemoteSocketRedaction is the redaction case of the conformance suite (ROADMAP 2.2
// *Snapshots and flags*): a peer snapshot that carries auth, the three headers in mixed
// case, x-api-key and a query token loses the first two groups and keeps the other two
// unchanged.
func TestRemoteSocketRedaction(t *testing.T) {
	in := json.RawMessage(nodeHandshake)
	out, err := socketio.RedactHandshake(in)
	if err != nil {
		t.Fatalf("RedactHandshake: %v", err)
	}
	got := decodeObject(t, out)
	if _, ok := got["auth"]; ok {
		t.Errorf("auth survived: %s", out)
	}
	headers := decodeObject(t, got["headers"])
	for _, name := range []string{"Authorization", "COOKIE", "Proxy-Authorization"} {
		if _, ok := headers[name]; ok {
			t.Errorf("header %s survived: %s", name, out)
		}
	}
	want := decodeObject(t, in)
	wantHeaders := decodeObject(t, want["headers"])
	for _, name := range []string{"x-api-key", "Set-Cookie", "host"} {
		if string(headers[name]) != string(wantHeaders[name]) {
			t.Errorf("header %s = %s, want %s", name, headers[name], wantHeaders[name])
		}
	}
	if len(headers) != 3 {
		t.Errorf("headers has %d entries, want 3: %s", len(headers), got["headers"])
	}
	for _, key := range []string{"time", "address", "xdomain", "secure", "issued", "url", "query"} {
		if string(got[key]) != string(want[key]) {
			t.Errorf("%s = %s, want %s", key, got[key], want[key])
		}
	}
	if len(got) != len(want)-1 {
		t.Errorf("object has %d keys, want %d", len(got), len(want)-1)
	}
	// The query token is a documented non-guarantee: it passes through.
	if !bytes.Contains(out, []byte("query-secret")) || !bytes.Contains(out, []byte("url-secret")) {
		t.Errorf("query or url value was altered: %s", out)
	}
	if !bytes.Equal(in, []byte(nodeHandshake)) {
		t.Error("input was modified")
	}
}

func TestRedactHandshake(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty object", `{}`, `{}`},
		{"nothing to omit", `{"url":"/","query":{}}`, `{"url":"/","query":{}}`},
		{"auth only", `{"auth":{"a":1}}`, `{}`},
		{"auth first", `{"auth":1,"url":"/"}`, `{"url":"/"}`},
		{"auth last", `{"url":"/","auth":1}`, `{"url":"/"}`},
		{"auth is any JSON value", `{"auth":null,"x":1}`, `{"x":1}`},
		{"auth key is exact", `{"Auth":1,"authx":2,"xauth":3}`, `{"Auth":1,"authx":2,"xauth":3}`},
		{"escaped auth key", `{"auth":1,"x":2}`, `{"x":2}`},
		{"duplicate auth keys", `{"auth":1,"x":0,"auth":2}`, `{"x":0}`},
		{"headers all three", `{"headers":{"authorization":"a","cookie":"b","proxy-authorization":"c"}}`, `{"headers":{}}`},
		{"headers mixed case", `{"headers":{"AUTHORIZATION":1,"Cookie":2,"pRoXy-AuThOrIzAtIoN":3,"keep":4}}`, `{"headers":{"keep":4}}`},
		{"headers lookalikes kept", `{"headers":{"set-cookie":1,"cookie2":2,"x-authorization":3,"proxy-authorization-x":4,"cooKie":5}}`, `{"headers":{"set-cookie":1,"cookie2":2,"x-authorization":3,"proxy-authorization-x":4}}`},
		{"kelvin sign is not cookie", "{\"headers\":{\"cooKie\":1}}", "{\"headers\":{\"cooKie\":1}}"},
		{"headers key is exact", `{"Headers":{"cookie":1}}`, `{"Headers":{"cookie":1}}`},
		{"headers null kept", `{"headers":null}`, `{"headers":null}`},
		{"headers string kept", `{"headers":"cookie"}`, `{"headers":"cookie"}`},
		{"headers array kept", `{"headers":[{"cookie":1}]}`, `{"headers":[{"cookie":1}]}`},
		{"nested objects untouched", `{"query":{"auth":1,"headers":{"cookie":2}},"data":{"auth":3}}`, `{"query":{"auth":1,"headers":{"cookie":2}},"data":{"auth":3}}`},
		{"duplicate headers objects", `{"headers":{"cookie":1,"a":1},"headers":{"authorization":2,"b":2}}`, `{"headers":{"a":1},"headers":{"b":2}}`},
		{"whitespace between members is dropped", " {\n \"a\" : 1 ,\n\t\"auth\" : 2 , \"b\" :\t[ 1 , 2 ] }\n", `{"a":1,"b":[ 1 , 2 ]}`},
		{"key and value bytes are kept", `{"kéy":"é","n":1.50e+2,"big":12345678901234567890123,"s":"<>& "}`, `{"kéy":"é","n":1.50e+2,"big":12345678901234567890123,"s":"<>& "}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := json.RawMessage(tc.in)
			got, err := socketio.RedactHandshake(in)
			if err != nil {
				t.Fatalf("RedactHandshake(%s): %v", tc.in, err)
			}
			if string(got) != tc.want {
				t.Errorf("RedactHandshake(%s)\n got %s\nwant %s", tc.in, got, tc.want)
			}
			if string(in) != tc.in {
				t.Errorf("input modified: %s", in)
			}
			again, err := socketio.RedactHandshake(got)
			if err != nil || string(again) != string(got) {
				t.Errorf("not idempotent: %s, %v", again, err)
			}
		})
	}
}

func TestRedactHandshakeNil(t *testing.T) {
	got, err := socketio.RedactHandshake(nil)
	if got != nil || err != nil {
		t.Errorf("RedactHandshake(nil) = %v, %v; want nil, nil", got, err)
	}
}

func TestRedactHandshakeErrors(t *testing.T) {
	for _, in := range []string{
		``, ` `, `null`, `true`, `1`, `"s"`, `[]`, `[{"auth":1}]`,
		`{`, `{"a"`, `{"a":`, `{"a":1`, `{"a":1,}`, `{"a" 1}`, `{1:2}`, `{"a":1}}`, `{"a":1}{}`, `{"a":1} x`,
		`{"a":tru}`, `{"headers":{"cookie":}}`, `{"headers":{"cookie":1`,
	} {
		out, err := socketio.RedactHandshake(json.RawMessage(in))
		if err == nil || out != nil {
			t.Errorf("RedactHandshake(%q) = %q, %v; want an error and nil", in, out, err)
		}
	}
}

func TestRedactHandshakeResultIsOwned(t *testing.T) {
	in := json.RawMessage(`{"a":"xyz","headers":{"h":"abc"}}`)
	out, err := socketio.RedactHandshake(in)
	if err != nil {
		t.Fatal(err)
	}
	for i := range out {
		out[i] = '#'
	}
	if string(in) != `{"a":"xyz","headers":{"h":"abc"}}` {
		t.Errorf("result aliases the input: %s", in)
	}
}

func TestRedactHandshakeLargeAndDeep(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"headers":{`)
	for i := 0; i < 2000; i++ {
		b.WriteString(`"h`)
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString(`":1,`)
	}
	b.WriteString(`"cookie":"c"},"query":`)
	b.WriteString(strings.Repeat("[", 500) + strings.Repeat("]", 500))
	b.WriteString(`,"auth":{}}`)
	out, err := socketio.RedactHandshake(json.RawMessage(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte(`"cookie"`)) || bytes.Contains(out, []byte(`"auth"`)) {
		t.Error("credential key survived")
	}
	if !bytes.Contains(out, []byte(strings.Repeat("[", 500))) {
		t.Error("deep value lost")
	}
}

// FuzzRedactHandshake checks, against a decode-based oracle, that every input either
// fails or yields a JSON object without auth and without the three headers whose other
// top-level keys are the input's.
func FuzzRedactHandshake(f *testing.F) {
	f.Add([]byte(nodeHandshake))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"headers":{"Cookie":1},"auth":2}`))
	f.Add([]byte(`{"auth":1,"headers":[]}`))
	f.Add([]byte(` {"a" : [1, {"auth":2}] } `))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, in []byte) {
		orig := bytes.Clone(in)
		out, err := socketio.RedactHandshake(in)
		if !bytes.Equal(in, orig) {
			t.Fatal("input modified")
		}
		var src map[string]json.RawMessage
		srcErr := json.Unmarshal(in, &src)
		if err != nil {
			if out != nil {
				t.Fatalf("error with non-nil result %q", out)
			}
			return
		}
		if in == nil {
			if out != nil {
				t.Fatalf("nil input gave %q", out)
			}
			return
		}
		if srcErr != nil {
			t.Fatalf("accepted %q that is not a JSON object: %v", in, srcErr)
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("result %q is not a JSON object: %v", out, err)
		}
		if _, ok := got["auth"]; ok {
			t.Fatalf("auth survived in %q", out)
		}
		for key := range got {
			if _, ok := src[key]; !ok {
				t.Fatalf("key %q appeared", key)
			}
		}
		for key := range src {
			if _, ok := got[key]; !ok && key != "auth" {
				t.Fatalf("key %q lost", key)
			}
		}
		var headers map[string]json.RawMessage
		if json.Unmarshal(got["headers"], &headers) == nil {
			for name := range headers {
				switch asciiLower(name) {
				case "authorization", "cookie", "proxy-authorization":
					t.Fatalf("header %q survived in %q", name, out)
				}
			}
		}
		again, err := socketio.RedactHandshake(out)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("not idempotent: %q then %q (%v)", out, again, err)
		}
	})
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}
