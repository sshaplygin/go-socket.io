package session

import (
	"io"
	"net/http"
	"sync"

	"github.com/sshaplygin/go-socket.io/engineio/internal"
)

func init() {
	internal.ObserveHeaders = func(s io.Closer, f func(*http.Request, http.Header)) {
		s.(*Session).headers = f
	}
}

// responseObserver runs on the first writer's goroutine, after the transport
// has prepared headers. FlushOut joins that writer before ServeHTTP returns.
type responseObserver struct {
	http.ResponseWriter
	session *Session
	request *http.Request
	once    sync.Once
	flushed bool
}

func (w *responseObserver) observe(status int) {
	w.once.Do(func() {
		if w.session.headers != nil {
			w.session.headers(w.request, w.Header())
		}
		if w.request.Method == http.MethodGet && status == http.StatusOK && w.session.Transport() == "polling" {
			w.flushed = true
			if h := w.session.hooks; h != nil && h.Flush != nil {
				h.Flush(w.session)
			}
		}
	})
}

func (w *responseObserver) WriteHeader(status int) {
	w.observe(status)
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseObserver) Write(p []byte) (int, error) {
	w.observe(http.StatusOK)
	return w.ResponseWriter.Write(p)
}

func (w *responseObserver) drain() {
	if h := w.session.hooks; w.flushed && h != nil && h.Drain != nil {
		h.Drain(w.session)
	}
}
