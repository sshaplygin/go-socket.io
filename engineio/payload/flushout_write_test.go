package payload

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

// gatedWriter blocks its first Write until release is closed and counts the writes
// that ended after FlushOut had returned.
type gatedWriter struct {
	started, release chan struct{}
	once             sync.Once
	returned         atomic.Bool
	late             atomic.Int32
}

func (w *gatedWriter) Write(p []byte) (int, error) {
	w.once.Do(func() {
		close(w.started)
		<-w.release
	})
	if w.returned.Load() {
		w.late.Add(1)
	}
	return len(p), nil
}

// TestPayloadFlushOutWaitsForWriter checks that FlushOut does not return while the writer
// is still writing to w when the write deadline passes: no write reaches w after it returned.
func TestPayloadFlushOutWaitsForWriter(t *testing.T) {
	p := New(true)
	w := &gatedWriter{started: make(chan struct{}), release: make(chan struct{})}
	deadline := time.Now().Add(stillOpenWindow)
	require.NoError(t, p.SetWriteDeadline(deadline))
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		if nw, err := p.NextWriter(frame.String, packet.MESSAGE); err == nil {
			_, _ = nw.Write([]byte("hello"))
			_ = nw.Close()
		}
	}()

	var flushErr error
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		flushErr = p.FlushOut(w)
		w.returned.Store(true)
	}()

	<-w.started // the deadline passes while the writer is inside w.Write
	select {
	case <-flushed:
		t.Error("FlushOut returned while the writer was still writing to w")
	case <-time.After(time.Until(deadline) + stillOpenWindow):
	}
	close(w.release)
	<-flushed

	require.Error(t, flushErr) // the deadline passed: FlushOut reports the timeout
	<-writerDone
	assert.Zero(t, w.late.Load(), "writes reached w after FlushOut returned")
}
