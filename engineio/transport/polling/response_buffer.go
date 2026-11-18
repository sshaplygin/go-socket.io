package polling

import (
	"bytes"
	"net/http"
)

const (
	// GET buffers and gzip writers share the eight-entry process-wide budget.
	maxRetainedPollingBuffers  = 8
	maxRetainedResponseBuffers = maxRetainedPollingBuffers - maxRetainedGzipWriters
	maxResponseBufferSize      = 2 << 20
)

// responseBufferCache retains only idle GET buffers. A channel bounds the free
// list across every session and avoids retaining a buffer on an idle connection.
type responseBufferCache struct {
	free chan []byte
}

func newResponseBufferCache() *responseBufferCache {
	return &responseBufferCache{free: make(chan []byte, maxRetainedResponseBuffers)}
}

var responseBuffers = newResponseBufferCache()

func (c *responseBufferCache) take() []byte {
	select {
	case b := <-c.free:
		return b[:0]
	default:
		return nil
	}
}

func (c *responseBufferCache) put(b []byte) {
	if cap(b) == 0 || cap(b) > maxResponseBufferSize {
		return
	}
	select {
	case c.free <- b[:0]:
	default:
	}
}

func newResponseBuffer(w http.ResponseWriter) *responseBuffer {
	b := &responseBuffer{ResponseWriter: w}
	b.buf = *bytes.NewBuffer(responseBuffers.take())
	return b
}

// release is called after the handler has finished its network write. flushOut
// has already detached its session writer, so no other goroutine uses b.
// Clear both references even when the buffer was too large for the free list.
func (b *responseBuffer) release() {
	responseBuffers.put(b.buf.Bytes())
	b.buf = bytes.Buffer{}
	b.ResponseWriter = nil
}
