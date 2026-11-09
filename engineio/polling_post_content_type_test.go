package engineio_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// contentTypeSession is one open polling session whose messages are collected
// from the server side.
type contentTypeSession struct {
	url      string
	conn     engineio.Conn
	messages chan string
}

// openContentTypeSession performs the polling handshake and starts a reader
// that forwards every message the server session receives.
func openContentTypeSession(t *testing.T, s *engineio.Server, base string, b64 bool) *contentTypeSession {
	t.Helper()
	target := base + "/?EIO=3&transport=polling"
	if b64 {
		target += "&b64=1"
	}
	accepted := make(chan engineio.Conn, 1)
	go func() {
		c, err := s.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	resp, err := http.Get(target)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)
	var conn engineio.Conn
	select {
	case conn = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("session was not accepted after the handshake")
	}
	sess := &contentTypeSession{url: target + "&sid=" + conn.ID(), conn: conn, messages: make(chan string, 16)}
	go func() {
		for {
			_, r, err := conn.NextReader()
			if err != nil {
				return
			}
			b, err := io.ReadAll(r)
			_ = r.Close()
			if err != nil {
				return
			}
			sess.messages <- string(b)
		}
	}()
	return sess
}

// post sends body with a Content-Type that is set only when contentType is not
// nil, the way a plain net/http client or curl would.
func (sess *contentTypeSession) post(t *testing.T, contentType *string, body io.Reader) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, sess.url, body)
	require.NoError(t, err)
	if contentType != nil {
		req.Header.Set("Content-Type", *contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode, string(b)
}

func (sess *contentTypeSession) expectMessage(t *testing.T, want string) {
	t.Helper()
	select {
	case got := <-sess.messages:
		require.Equal(t, want, got)
	case <-time.After(5 * time.Second):
		t.Fatalf("message %q was not delivered", want)
	}
}

func (sess *contentTypeSession) expectNoMessage(t *testing.T) {
	t.Helper()
	select {
	case got := <-sess.messages:
		t.Fatalf("unexpected message %q", got)
	default:
	}
}

func contentTypeServer(t *testing.T, opts ...engineio.Option) (*engineio.Server, string) {
	t.Helper()
	s := engineio.NewServer(opts...)
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	return s, ts.URL
}

func str(s string) *string { return &s }

const (
	textHello   = "6:4hello"
	binaryHello = "\x00\x06\xff4hello"
)

// TestPollingPostTextContentTypes checks that every Content-Type other than
// application/octet-stream selects the text payload path, as in the reference
// implementation: a missing header, text/plain without a charset, other charsets
// and case or spacing variants, unrelated media types and malformed values.
func TestPollingPostTextContentTypes(t *testing.T) {
	cases := []struct {
		name        string
		contentType *string
	}{
		{"missing", nil},
		{"empty", str("")},
		{"text/plain", str("text/plain")},
		{"text/plain utf-8", str("text/plain; charset=utf-8")},
		{"text/plain UTF-8 no space", str("text/plain;charset=UTF-8")},
		{"upper case", str("TEXT/PLAIN; CHARSET=UTF-8")},
		{"spaces around parameter", str("text/plain ;  charset = utf-8")},
		{"quoted charset", str(`text/plain; charset="utf-8"`)},
		{"other charset", str("text/plain; charset=iso-8859-1")},
		{"extra parameter", str("text/plain; charset=utf-8; format=flowed")},
		{"form urlencoded (curl -d)", str("application/x-www-form-urlencoded")},
		{"json", str("application/json")},
		{"html", str("text/html")},
		{"multipart without boundary", str("multipart/form-data")},
		{"octet-stream prefix only", str("application/octet-streams")},
		{"vendor type", str("application/vnd.example+text")},
		{"no subtype", str("text")},
		{"slash only", str("/")},
		{"garbage", str("???")},
		{"unterminated quote", str(`text/plain; charset="utf-8`)},
		{"empty parameters", str("text/plain;;;;")},
		{"parameter without value", str("text/plain; charset")},
		{"parameters only", str("; charset=utf-8")},
	}
	for _, b64 := range []bool{true, false} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("b64=%v/%s", b64, tc.name), func(t *testing.T) {
				s, base := contentTypeServer(t)
				sess := openContentTypeSession(t, s, base, b64)
				status, body := sess.post(t, tc.contentType, strings.NewReader(textHello))
				require.Equal(t, http.StatusOK, status, body)
				require.Equal(t, "ok", body)
				sess.expectMessage(t, "hello")
			})
		}
	}
}

// TestPollingPostBinaryContentTypes checks that application/octet-stream,
// whatever its case, spacing or parameters, selects the binary payload path.
func TestPollingPostBinaryContentTypes(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
	}{
		{"plain", "application/octet-stream"},
		{"upper case", "Application/Octet-Stream"},
		{"with parameter", "application/octet-stream; charset=binary"},
		{"space before parameter", "application/octet-stream ; x=y"},
		{"malformed parameter", `application/octet-stream; x="y`},
		{"empty parameters", "application/octet-stream;;"},
	}
	for _, b64 := range []bool{true, false} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("b64=%v/%s", b64, tc.name), func(t *testing.T) {
				s, base := contentTypeServer(t)
				sess := openContentTypeSession(t, s, base, b64)
				status, body := sess.post(t, &tc.contentType, strings.NewReader(binaryHello))
				require.Equal(t, http.StatusOK, status, body)
				require.Equal(t, "ok", body)
				sess.expectMessage(t, "hello")
			})
		}
	}
}

// TestPollingPostBodyDecodesByContentType checks that the Content-Type, not the
// body, picks the decoder: a binary payload under a text type and a text payload
// under the binary type are not decodable and are answered HTTP 400.
func TestPollingPostBodyDecodesByContentType(t *testing.T) {
	cases := []struct {
		name        string
		contentType *string
		body        string
	}{
		{"binary body without Content-Type", nil, binaryHello},
		{"binary body as text/plain", str("text/plain; charset=utf-8"), binaryHello},
		{"binary body as form", str("application/x-www-form-urlencoded"), binaryHello},
		{"text body as octet-stream", str("application/octet-stream"), textHello},
		{"random bytes without Content-Type", nil, "\xff\xfe\x00\x01\x80"},
		{"random bytes as octet-stream", str("application/octet-stream"), "\xff\xfe\x00\x01\x80"},
		{"not a payload without Content-Type", nil, "not a payload"},
		{"not a payload as text/plain", str("text/plain; charset=utf-8"), "not a payload"},
		{"invalid base64 packet without Content-Type", nil, "5:b4!!!"},
		{"length beyond body without Content-Type", nil, "99:4hello"},
		{"huge length without Content-Type", nil, "99999999999999999999:4hello"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, base := contentTypeServer(t)
			sess := openContentTypeSession(t, s, base, true)
			status, body := sess.post(t, tc.contentType, strings.NewReader(tc.body))
			require.Equal(t, http.StatusBadRequest, status, body)
			sess.expectNoMessage(t)
		})
	}
}

// TestPollingPostMalformedPayloadKeepsErrorBehavior checks that a payload the
// decoder rejects ends the session the same way for every Content-Type.
func TestPollingPostMalformedPayloadKeepsErrorBehavior(t *testing.T) {
	for _, contentType := range []*string{nil, str("text/plain; charset=utf-8"), str("application/x-www-form-urlencoded")} {
		name := "missing"
		if contentType != nil {
			name = *contentType
		}
		t.Run(name, func(t *testing.T) {
			s, base := contentTypeServer(t)
			sess := openContentTypeSession(t, s, base, true)
			status, _ := sess.post(t, contentType, strings.NewReader("not a payload"))
			require.Equal(t, http.StatusBadRequest, status)
			// The session reader fails with the transport error and the session
			// is closed: a following POST is refused.
			deadline := time.After(5 * time.Second)
			for {
				status, _ = sess.post(t, contentType, strings.NewReader(textHello))
				if status != http.StatusOK {
					break
				}
				select {
				case <-sess.messages:
				case <-deadline:
					t.Fatal("session still accepts POSTs after a malformed payload")
				}
			}
			require.Equal(t, http.StatusBadRequest, status)
		})
	}
}

// TestPollingPostBodyLimitAppliesToEveryContentType checks that the request
// body limit is enforced before and while a body is read, whatever the
// Content-Type says, including a body of unknown length.
func TestPollingPostBodyLimitAppliesToEveryContentType(t *testing.T) {
	const limit = 64
	text := fmt.Sprintf("%d:4%s", limit+10, strings.Repeat("x", limit+9))
	binary := "\x00\x07\x04\xff4" + strings.Repeat("x", limit+9)
	cases := []struct {
		contentType *string
		body        string
	}{
		{nil, text},
		{str("text/plain"), text},
		{str("application/x-www-form-urlencoded"), text},
		{str("application/octet-stream"), binary},
	}
	for _, tc := range cases {
		name := "missing"
		if tc.contentType != nil {
			name = *tc.contentType
		}
		t.Run(name+"/declared length", func(t *testing.T) {
			s, base := contentTypeServer(t, engineio.WithMaxHTTPBufferSize(limit))
			sess := openContentTypeSession(t, s, base, true)
			status, _ := sess.post(t, tc.contentType, strings.NewReader(tc.body))
			require.Equal(t, http.StatusRequestEntityTooLarge, status)
		})
		t.Run(name+"/unknown length", func(t *testing.T) {
			s, base := contentTypeServer(t, engineio.WithMaxHTTPBufferSize(limit))
			sess := openContentTypeSession(t, s, base, true)
			status, _ := sess.post(t, tc.contentType, io.MultiReader(strings.NewReader(tc.body)))
			require.Equal(t, http.StatusRequestEntityTooLarge, status)
		})
	}
	t.Run("body at the limit is accepted without Content-Type", func(t *testing.T) {
		s, base := contentTypeServer(t, engineio.WithMaxHTTPBufferSize(limit))
		sess := openContentTypeSession(t, s, base, true)
		msg := strings.Repeat("y", limit-5)
		status, body := sess.post(t, nil, strings.NewReader(fmt.Sprintf("%d:4%s", len(msg)+1, msg)))
		require.Equal(t, http.StatusOK, status, body)
		sess.expectMessage(t, msg)
	})
}

// TestPollingPostRequestChecksIgnoreContentType checks the refusals that do not
// depend on the payload: wrong method and unknown session.
func TestPollingPostRequestChecksIgnoreContentType(t *testing.T) {
	s, base := contentTypeServer(t)
	sess := openContentTypeSession(t, s, base, true)

	for _, contentType := range []*string{nil, str("text/plain; charset=utf-8"), str("application/x-www-form-urlencoded")} {
		req, err := http.NewRequest(http.MethodPut, sess.url, strings.NewReader(textHello))
		require.NoError(t, err)
		if contentType != nil {
			req.Header.Set("Content-Type", *contentType)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		b, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		require.Equal(t, "invalid method\n", string(b))

		unknown := &contentTypeSession{url: base + "/?EIO=3&transport=polling&b64=1&sid=unknown"}
		status, body := unknown.post(t, contentType, strings.NewReader(textHello))
		require.Equal(t, http.StatusBadRequest, status)
		require.Contains(t, body, "Session ID unknown")
	}
	sess.expectNoMessage(t)
}

// TestPollingPlainHTTPClientRoundTrip drives a session with nothing but net/http
// and no Content-Type: a message is posted, the server echoes it, and the echo
// is read from the next poll.
func TestPollingPlainHTTPClientRoundTrip(t *testing.T) {
	for _, b64 := range []bool{true, false} {
		t.Run(fmt.Sprintf("b64=%v", b64), func(t *testing.T) {
			s, base := contentTypeServer(t)
			sess := openContentTypeSession(t, s, base, b64)
			status, body := sess.post(t, nil, strings.NewReader("12:4hello world"))
			require.Equal(t, http.StatusOK, status, body)
			sess.expectMessage(t, "hello world")

			// The write waits for a poll, so it runs beside the GET below.
			written := make(chan error, 1)
			go func() {
				w, err := sess.conn.NextWriter(eioparser.String)
				if err == nil {
					_, err = w.Write([]byte("echo: hello world"))
				}
				if err == nil {
					err = w.Close()
				}
				written <- err
			}()

			resp, err := http.Get(sess.url)
			require.NoError(t, err)
			b, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.NoError(t, <-written)
			if b64 {
				require.Equal(t, "18:4echo: hello world", string(b))
			} else {
				require.Equal(t, "\x00\x01\x08\xff4echo: hello world", string(b))
			}
		})
	}
}
