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

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
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

// attrHandler records every record together with the attributes added
// through With, keyed by attribute name.
type attrHandler struct {
	mu    *sync.Mutex
	recs  *[]map[string]string
	attrs []slog.Attr
}

func newAttrHandler() *attrHandler {
	return &attrHandler{mu: &sync.Mutex{}, recs: &[]map[string]string{}}
}

func (h *attrHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *attrHandler) Handle(_ context.Context, r slog.Record) error {
	m := map[string]string{"msg": r.Message}
	for _, a := range h.attrs {
		m[a.Key] = a.Value.String()
	}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.String()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.recs = append(*h.recs, m)
	return nil
}

func (h *attrHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *attrHandler) WithGroup(string) slog.Handler { return h }

func (h *attrHandler) last() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return (*h.recs)[len(*h.recs)-1]
}

// TestSessionLogAttributes checks that session records carry sid and the
// current transport, and that the transport follows an upgrade.
func TestSessionLogAttributes(t *testing.T) {
	h := newAttrHandler()
	s, err := New(failingConn{}, "sid7", "polling",
		transport.ConnParameters{PingTimeout: time.Second}, slog.New(h))
	require.NoError(t, err)

	s.logger().Error("before upgrade")
	require.Equal(t, "sid7", h.last()["sid"])
	require.Equal(t, "polling", h.last()["transport"])

	s.switchTransport("websocket", failingConn{})
	s.logger().Error("after upgrade")
	require.Equal(t, "sid7", h.last()["sid"])
	require.Equal(t, "websocket", h.last()["transport"])
	require.Equal(t, "websocket", s.Transport())
}
