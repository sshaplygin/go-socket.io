package session

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/googollee/go-socket.io/engineio/frame"
	"github.com/googollee/go-socket.io/engineio/packet"
	"github.com/googollee/go-socket.io/engineio/transport"
)

// recordingHandler is a slog.Handler that keeps every record it receives.
type recordingHandler struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recs = append(h.recs, r)
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) errValues() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, r := range h.recs {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "err" {
				out = append(out, a.Value.String())
			}
			return true
		})
	}
	return out
}

// failingConn is a transport.Conn whose deadline and close calls fail.
type failingConn struct {
	deadlineErr error
	closeErr    error
}

func (c failingConn) NextReader() (frame.Type, packet.Type, io.ReadCloser, error) {
	return 0, 0, nil, io.EOF
}

func (c failingConn) NextWriter(frame.Type, packet.Type) (io.WriteCloser, error) {
	return nil, io.EOF
}

func (c failingConn) Close() error                     { return c.closeErr }
func (c failingConn) URL() url.URL                     { return url.URL{} }
func (c failingConn) LocalAddr() net.Addr              { return nil }
func (c failingConn) RemoteAddr() net.Addr             { return nil }
func (c failingConn) RemoteHeader() http.Header        { return nil }
func (c failingConn) SetReadDeadline(time.Time) error  { return c.deadlineErr }
func (c failingConn) SetWriteDeadline(time.Time) error { return c.deadlineErr }

var _ transport.Conn = failingConn{}

// TestNewReportsCloseErrorToLogger checks that New uses the logger it is
// given: when the initial deadline cannot be set and closing the transport
// fails too, the close error is reported through that logger.
func TestNewReportsCloseErrorToLogger(t *testing.T) {
	h := &recordingHandler{}
	deadlineErr := errors.New("deadline failed")
	closeErr := errors.New("close failed")

	s, err := New(failingConn{deadlineErr: deadlineErr, closeErr: closeErr},
		"sid", "polling", transport.ConnParameters{PingTimeout: time.Second}, slog.New(h))

	require.Nil(t, s)
	require.ErrorIs(t, err, deadlineErr)
	require.Contains(t, h.errValues(), closeErr.Error())
}

// TestNewNilLoggerDefaults checks that a nil logger is accepted and does not
// panic when New has to report an error.
func TestNewNilLoggerDefaults(t *testing.T) {
	deadlineErr := errors.New("deadline failed")

	s, err := New(failingConn{deadlineErr: deadlineErr, closeErr: errors.New("close failed")},
		"sid", "polling", transport.ConnParameters{PingTimeout: time.Second}, nil)

	require.Nil(t, s)
	require.ErrorIs(t, err, deadlineErr)
}
