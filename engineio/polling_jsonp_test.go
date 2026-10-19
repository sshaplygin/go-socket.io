package engineio

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// pollingHandshake opens a polling session with the extra query and returns the
// handshake response, its body and the session id.
func pollingHandshake(t *testing.T, base, query string) (*http.Response, []byte, string) {
	t.Helper()
	resp, err := http.Get(base + "/?EIO=3&transport=polling" + query)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)
	pkts := nodeBinaryPayload(t, body)
	require.NotEmpty(t, pkts)
	require.Equal(t, byte('0'), pkts[0][0], "OPEN packet expected: %q", pkts[0])
	var open struct {
		SID string `json:"sid"`
	}
	require.NoError(t, json.Unmarshal(pkts[0][1:], &open))
	return resp, body, open.SID
}

// JSONP is removed (D4): the `j` parameter is ignored, never written into a response.
func TestPollingIgnoresJSONPCallback(t *testing.T) {
	must := require.New(t)
	should := assert.New(t)

	svr := NewServer()
	defer func() { must.NoError(svr.Close()) }()
	httpSvr := httptest.NewServer(svr)
	defer httpSvr.Close()

	const j = "&j=alert(document.domain)//"
	resp, body, sid := pollingHandshake(t, httpSvr.URL, j)
	should.Equal("application/octet-stream", resp.Header.Get("Content-Type"))
	should.NotContains(string(body), "___eio")
	should.NotContains(string(body), "alert")

	req, err := http.NewRequest(http.MethodOptions, httpSvr.URL+"/?EIO=3&transport=polling&sid="+sid+j, nil)
	must.NoError(err)
	resp, err = http.DefaultClient.Do(req)
	must.NoError(err)
	must.NoError(resp.Body.Close())
	should.Equal(http.StatusOK, resp.StatusCode)
	should.Equal("Content-Type", resp.Header.Get("Access-Control-Allow-Headers"))
}

// JSONP is removed (D4): a form POST (`d=`) is rejected and never reaches the session.
func TestPollingRejectsJSONPFormPost(t *testing.T) {
	must := require.New(t)
	should := assert.New(t)

	svr := NewServer()
	defer func() { must.NoError(svr.Close()) }()
	httpSvr := httptest.NewServer(svr)
	defer httpSvr.Close()

	_, _, sid := pollingHandshake(t, httpSvr.URL, "")
	conn, err := svr.Accept()
	must.NoError(err)
	defer func() { _ = conn.Close() }()

	poll := httpSvr.URL + "/?EIO=3&transport=polling&sid=" + sid
	for _, u := range []string{poll, poll + "&j=0"} {
		resp, err := http.Post(u, "application/x-www-form-urlencoded", strings.NewReader("d=6%3A4form"))
		must.NoError(err)
		must.NoError(resp.Body.Close())
		should.Equal(http.StatusBadRequest, resp.StatusCode, u)
	}

	posted := make(chan int, 1)
	go func() {
		resp, err := http.Post(poll, "text/plain;charset=UTF-8", strings.NewReader("5:4text"))
		if err != nil {
			posted <- 0
			return
		}
		_ = resp.Body.Close()
		posted <- resp.StatusCode
	}()
	ft, r, err := conn.NextReader()
	must.NoError(err)
	b, err := io.ReadAll(r)
	must.NoError(err)
	must.NoError(r.Close())
	should.Equal(eioparser.String, ft)
	should.Equal("text", string(b))
	should.Equal(http.StatusOK, <-posted)
}
