package polling

import (
	"compress/gzip"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPollingResponseAndGzipCachesShareRetentionBudget(t *testing.T) {
	const retainedPollingBudget = 8
	require.Equal(t, retainedPollingBudget, maxRetainedResponseBuffers+maxRetainedGzipWriters)
	responses := newResponseBufferCache()
	gzipWriters := newGzipWriterCache()

	// Fill the GET cache from more than eight completed concurrent responses.
	for i := 0; i < 12; i++ {
		responses.put(make([]byte, 512*1024+i))
	}
	responses.put(make([]byte, maxResponseBufferSize+1))

	// Keep the writers checked out until all have been created, as a burst of
	// concurrent compressed responses would, then finish and return them.
	var writers [8]*gzip.Writer
	for i := range writers {
		var err error
		writers[i], err = gzipWriters.take(io.Discard)
		require.NoError(t, err)
	}
	for _, writer := range writers {
		require.NoError(t, writer.Close())
		gzipWriters.put(writer)
	}

	require.LessOrEqual(t, len(responses.free)+len(gzipWriters.free), retainedPollingBudget)
	n := len(responses.free)
	for i := 0; i < n; i++ {
		b := <-responses.free
		require.LessOrEqual(t, cap(b), maxResponseBufferSize)
	}
}
