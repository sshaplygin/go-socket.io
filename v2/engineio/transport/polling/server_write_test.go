package polling

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
)

// gatedResponse is a ResponseWriter whose first Write blocks until release is closed;
// it counts the Header and WriteHeader calls made after that Write started.
type gatedResponse struct {
	header  http.Header
	started chan struct{}
	release chan struct{}
	wrote   atomic.Bool
	late    atomic.Int32
}

func (w *gatedResponse) Header() http.Header {
	if w.wrote.Load() {
		w.late.Add(1)
	}
	return w.header
}

func (w *gatedResponse) WriteHeader(int) { w.Header() }
func (w *gatedResponse) Write(p []byte) (int, error) {
	if w.wrote.CompareAndSwap(false, true) {
		close(w.started)
		<-w.release
	}
	return len(p), nil
}

// TestServerGetFlushErrorDuringWrite checks that when the write deadline passes while the
// session writer is writing to the response, the GET handler neither returns before
// the Write ends nor starts a second response after the partial one.
func TestServerGetFlushErrorDuringWrite(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?transport=polling", nil)
	conn := newServerConn(Default, req)
	w := &gatedResponse{header: http.Header{}, started: make(chan struct{}), release: make(chan struct{})}

	deadline := time.Now().Add(100 * time.Millisecond)
	require.NoError(t, conn.SetWriteDeadline(deadline))

	go func() {
		if nw, err := conn.NextWriter(frame.String, packet.MESSAGE); err == nil {
			_, _ = nw.Write([]byte("hello"))
			_ = nw.Close()
		}
	}()

	served := make(chan struct{})
	go func() {
		defer close(served)
		conn.ServeHTTP(w, req)
	}()

	<-w.started // the deadline passes while the writer is inside w.Write
	select {
	case <-served:
		t.Error("ServeHTTP returned while the session writer was still writing")
	case <-time.After(time.Until(deadline) + 100*time.Millisecond):
	}
	close(w.release)
	<-served

	assert.Zero(t, w.late.Load(), "the handler used the ResponseWriter after the response was started")
}
