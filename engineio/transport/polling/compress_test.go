package polling

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"

	"github.com/stretchr/testify/require"
)

type failingCompressionResponse struct {
	*httptest.ResponseRecorder
	err error
}

func (w failingCompressionResponse) Write([]byte) (int, error) { return 0, w.err }

// Covers E9
func TestPollingCompressionWriteFailure(t *testing.T) {
	for _, encoding := range []string{"", "gzip", "deflate"} {
		t.Run(encoding, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://localhost/?b64=1", nil)
			req.Header.Set("Accept-Encoding", encoding)
			c := newServerConn(&Transport{Compression: true}, req)
			defer func() { _ = c.Close() }()
			require.NoError(t, c.SetWriteDeadline(time.Now().Add(time.Second)))
			failure := errors.New("HTTP send failed")
			response := failingCompressionResponse{httptest.NewRecorder(), failure}
			done := make(chan error, 1)
			go func() {
				w, err := c.NextWriter(eioparser.String, eioparser.MESSAGE)
				if err == nil {
					_, err = io.WriteString(w, strings.Repeat("a", 4096))
					if closeErr := w.Close(); err == nil {
						err = closeErr
					}
				}
				done <- err
			}()
			c.ServeHTTP(response, req)
			var op *OpError
			require.ErrorAs(t, <-done, &op)
			require.Same(t, failure, op.Err)
			require.Empty(t, response.Body.String(), "a failed started response must not receive another HTTP error body")
		})
	}
}

// Covers E9
func TestPollingCompressionFlagAndNoop(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://localhost/?b64=1", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	c := newServerConn(&Transport{Compression: true, CompressionThreshold: 1}, req)
	defer func() { _ = c.Close() }()
	require.NoError(t, c.SetWriteDeadline(time.Now().Add(time.Second)))
	done := make(chan error, 1)
	go func() {
		w, err := c.NextWriterCompress(eioparser.String, eioparser.MESSAGE, false)
		if err == nil {
			_, err = io.WriteString(w, "1:6")
			if closeErr := w.Close(); err == nil {
				err = closeErr
			}
		}
		done <- err
	}()
	response := httptest.NewRecorder()
	c.ServeHTTP(response, req)
	require.NoError(t, <-done)
	require.Empty(t, response.Header().Get("Content-Encoding"))
	require.Equal(t, "4:41:6", response.Body.String())
	response = httptest.NewRecorder()
	polled := make(chan struct{})
	go func() { c.ServeHTTP(response, req); close(polled) }()
	require.Eventually(t, c.AnswerPoll, time.Second, time.Millisecond)
	<-polled
	require.Equal(t, "gzip", response.Header().Get("Content-Encoding"), "transport NOOP uses the default compression flag")
}
