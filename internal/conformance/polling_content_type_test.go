//go:build conformance

package conformance

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// pollingPostContentTypes are the Content-Type values of a polling POST for
// which the reference server (engine.io 3.6.2) and this library agree: the exact
// application/octet-stream selects the binary payload, anything else the text
// payload. The case and parameter variants of application/octet-stream are a
// documented Go difference (docs/PROTOCOL.md) and are covered by unit tests.
var pollingPostContentTypes = []struct {
	name        string
	contentType *string
	binary      bool
}{
	{"missing", nil, false},
	{"text/plain", ptr("text/plain"), false},
	{"text/plain charset", ptr("text/plain;charset=UTF-8"), false},
	{"text/plain other charset", ptr("text/plain; charset=iso-8859-1"), false},
	{"form urlencoded", ptr("application/x-www-form-urlencoded"), false},
	{"json", ptr("application/json"), false},
	{"octet-stream", ptr("application/octet-stream"), true},
}

func ptr(s string) *string { return &s }

func postWithContentType(t *testing.T, target string, contentType *string, body string) response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(body))
	require.NoError(t, err)
	if contentType != nil {
		req.Header.Set("Content-Type", *contentType)
	}
	r, err := rawHTTP.Do(req)
	require.NoError(t, err)
	defer func() { _ = r.Body.Close() }()
	b, err := io.ReadAll(r.Body)
	return response{status: r.StatusCode, header: r.Header, body: b, err: err}
}

// pollingEcho opens a polling session, posts the Socket.IO event echo(value)
// with an acknowledgement ID as a payload of the given kind and returns the
// packets of the polls that follow, up to the acknowledgement.
func pollingEcho(t *testing.T, base string, contentType *string, binary bool, value string) (postStatus int, postBody string, packets []string) {
	t.Helper()
	// The session is always opened as text: the reference answers a binary-capable
	// handshake in a different frame layout. Only the request body is binary.
	poll := openPollingMode(t, base, false)
	event := fmt.Sprintf(`421["echo",%q]`, value)
	body := fmt.Sprintf("%d:%s", len(event), event)
	if binary {
		body = "\x00" + lengthDigits(len(event)) + "\xff" + event
	}
	post := postWithContentType(t, poll, contentType, body)
	require.NoError(t, post.err)
	for i := 0; i < 3; i++ {
		r := request(http.MethodGet, poll, nil)
		require.NoError(t, r.err)
		require.Equal(t, http.StatusOK, r.status, "%s", r.body)
		packets = append(packets, pollPackets(t, r.body, false)...)
		for _, p := range packets {
			if strings.HasPrefix(p, "431") {
				return post.status, string(post.body), packets
			}
		}
	}
	return post.status, string(post.body), packets
}

// lengthDigits spells n as the binary payload does: one byte per decimal digit.
func lengthDigits(n int) string {
	var out []byte
	for _, d := range fmt.Sprint(n) {
		out = append(out, byte(d-'0'))
	}
	return string(out)
}

// TestPollingPostContentTypeMatchesReference posts the same Socket.IO event
// with each Content-Type to this library and to the reference Node server and
// requires both to accept it and to acknowledge it.
func TestPollingPostContentTypeMatchesReference(t *testing.T) {
	servers := []struct {
		name string
		url  func(*testing.T) string
	}{
		{"go", func(t *testing.T) string { return differentialGoServer(t).URL + "/socket.io" }},
		// The reference serves Socket.IO only under /socket.io.
		{"node", func(t *testing.T) string { return differentialReference(t) + "/socket.io" }},
	}
	for _, server := range servers {
		for _, tc := range pollingPostContentTypes {
			t.Run(server.name+"/"+tc.name, func(t *testing.T) {
				status, body, packets := pollingEcho(t, server.url(t), tc.contentType, tc.binary, tc.name)
				require.Equal(t, http.StatusOK, status, body)
				require.Equal(t, "ok", body)
				// This library ends a JSON packet with a newline; it is insignificant.
				var trimmed []string
				for _, p := range packets {
					trimmed = append(trimmed, strings.TrimSpace(p))
				}
				require.Contains(t, trimmed, fmt.Sprintf(`431[%q]`, tc.name))
			})
		}
	}
}

// TestNodeHTTPClientPostsWithoutContentType drives this library's polling
// transport from Node's http module, which sends no Content-Type of its own.
func TestNodeHTTPClientPostsWithoutContentType(t *testing.T) {
	ts := differentialGoServer(t)
	runNode(t, "testdata/node/polling-post.js", ts.URL)
}
