package payload

import (
	"io"
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
// is still writing to w when the write deadline passes or the payload is closed: no write
// reaches w after it returned, and Close ends FlushOut with io.EOF well before the deadline.
func TestPayloadFlushOutWaitsForWriter(t *testing.T) {
	const closeDeadline = 5 * time.Second
	for _, tc := range []struct {
		name     string
		deadline time.Duration
		trigger  func(*Payload) // runs once the writer is inside w.Write
		check    func(*testing.T, error)
	}{
		{"deadline", stillOpenWindow, func(*Payload) { time.Sleep(2 * stillOpenWindow) },
			func(t *testing.T, err error) { require.Error(t, err) }}, // the timeout
		{"close", closeDeadline, func(p *Payload) { require.NoError(t, p.Close()) },
			func(t *testing.T, err error) { assert.Equal(t, io.EOF, err) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := New(true)
			w := &gatedWriter{started: make(chan struct{}), release: make(chan struct{})}
			require.NoError(t, p.SetWriteDeadline(time.Now().Add(tc.deadline)))
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
			start := time.Now()
			go func() {
				defer close(flushed)
				flushErr = p.FlushOut(w)
				w.returned.Store(true)
			}()

			<-w.started
			tc.trigger(p)
			select {
			case <-flushed:
				t.Error("FlushOut returned while the writer was still writing to w")
			case <-time.After(stillOpenWindow):
			}
			close(w.release)
			select {
			case <-flushed:
			case <-time.After(closeDeadline / 2):
				t.Fatal("FlushOut did not return once the Write ended")
			}

			tc.check(t, flushErr)
			assert.Less(t, time.Since(start), closeDeadline/2, "FlushOut waited for the deadline")
			<-writerDone
			assert.Zero(t, w.late.Load(), "writes reached w after FlushOut returned")
		})
	}
}
