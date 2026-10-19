package polling

import (
	"bytes"
	"compress/gzip"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/stretchr/testify/require"
	"io"
	"net/http/httptest"
	"testing"
	"time"
)

// Covers: G24
func TestPollingBatchesOrderedPacketsInOneResponse(t *testing.T) {
	for _, encoding := range []string{"", "gzip"} {
		t.Run(encoding, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://localhost/?b64=1", nil)
			req.Header.Set("Accept-Encoding", encoding)
			c := newServerConn(&Transport{Compression: true, CompressionThreshold: 1}, req)
			defer c.Close()
			require.NoError(t, c.SetWriteDeadline(time.Now().Add(time.Second)))
			done := make(chan error, 1)
			go func() {
				for _, body := range []string{"first", "second", "third"} {
					w, e := c.NextWriter(eioparser.String, eioparser.MESSAGE)
					if e != nil {
						done <- e
						return
					}
					_, e = io.WriteString(w, body)
					if e == nil {
						e = w.Close()
					}
					if e != nil {
						done <- e
						return
					}
				}
				done <- nil
			}()
			response := httptest.NewRecorder()
			c.ServeHTTP(response, req)
			body := response.Body.Bytes()
			if encoding != "" {
				r, e := gzip.NewReader(bytes.NewReader(body))
				require.NoError(t, e)
				body, e = io.ReadAll(r)
				require.NoError(t, e)
				require.NoError(t, r.Close())
			}
			require.Equal(t, "6:4first7:4second6:4third", string(body))
			require.NoError(t, <-done)
		})
	}
}
