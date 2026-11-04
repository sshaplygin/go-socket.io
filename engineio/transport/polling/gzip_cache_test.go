package polling

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGzipWriterCacheReuseAndRetention(t *testing.T) {
	cache := newGzipWriterCache()
	const count = maxRetainedGzipWriters + 8
	var outputs [count]bytes.Buffer
	var writers [count]*gzip.Writer
	seen := make(map[*gzip.Writer]bool, count)
	for i := range writers {
		var err error
		writers[i], err = cache.take(&outputs[i])
		require.NoError(t, err)
		require.False(t, seen[writers[i]], "a checked-out writer must not be reused")
		seen[writers[i]] = true
		_, err = fmt.Fprintf(writers[i], "packet-%d", i)
		require.NoError(t, err)
	}
	for i, writer := range writers {
		require.NoError(t, writer.Close())
		cache.put(writer)
		reader, err := gzip.NewReader(bytes.NewReader(outputs[i].Bytes()))
		require.NoError(t, err)
		body, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		require.Equal(t, fmt.Sprintf("packet-%d", i), string(body))
	}
	require.Len(t, cache.free, maxRetainedGzipWriters)

	var next bytes.Buffer
	reused, err := cache.take(&next)
	require.NoError(t, err)
	require.True(t, seen[reused], "a completed writer should be reused")
	_, err = io.WriteString(reused, "short")
	require.NoError(t, err)
	require.NoError(t, reused.Close())
	cache.put(reused)
	reader, err := gzip.NewReader(bytes.NewReader(next.Bytes()))
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, "short", string(body), "the earlier response must not leak into the next")
}

func TestResponseWriterGzipConcurrentReuse(t *testing.T) {
	const workers, rounds = 16, 8
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for round := 0; round < rounds; round++ {
				packet := fmt.Sprintf("4worker-%d-round-%d", worker, round)
				frame := []byte(fmt.Sprintf("%d:%s", len(packet), packet))
				out := httptest.NewRecorder()
				conn := &serverConn{transport: &Transport{Compression: true, CompressionThreshold: 1}}
				writer := &responseWriter{w: out, conn: conn, encoding: "gzip"}
				_, err := writer.Write(frame)
				if err != nil {
					t.Errorf("write: %v", err)
					return
				}
				if err := writer.Close(); err != nil {
					t.Errorf("close: %v", err)
					return
				}
				if got := out.Header().Get("Content-Encoding"); got != "gzip" {
					t.Errorf("encoding: %q", got)
					return
				}
				reader, err := gzip.NewReader(bytes.NewReader(out.Body.Bytes()))
				if err != nil {
					t.Errorf("gzip reader: %v", err)
					return
				}
				got, err := io.ReadAll(reader)
				_ = reader.Close()
				if err != nil || !bytes.Equal(got, frame) {
					t.Errorf("round trip: got %q, err %v; want %q", got, err, frame)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
}
