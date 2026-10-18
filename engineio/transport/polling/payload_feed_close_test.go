package polling

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFeedInCloseAfterReaderAcquired(t *testing.T) {
	p := newPayload(false)
	done := make(chan error, 1)
	go func() { done <- p.FeedIn(bytes.NewBufferString("1:0"), false) }()
	_, _, r, err := p.NextReader()
	require.NoError(t, err)
	require.NoError(t, p.Close())
	// Closing the reader after the payload closed cannot send readError.
	require.ErrorIs(t, r.Close(), io.EOF)
	select {
	case err := <-done:
		require.ErrorIs(t, err, io.EOF)
	case <-time.After(time.Second):
		t.Fatal("FeedIn did not return after Close")
	}
}
