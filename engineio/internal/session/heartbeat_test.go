package session

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func TestPingTimeoutIdleClient(t *testing.T) {
	for _, name := range []string{"ping", "messages", "silent"} {
		t.Run(name, func(t *testing.T) {
			accepted := make(chan *Session, 1)
			done := make(chan error, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := websocket.Default.Accept(w, r)
				if err != nil {
					done <- err
					return
				}
				s, err := New(c, "heartbeat", "websocket", eioparser.ConnParameters{
					PingInterval: time.Second, PingTimeout: 500 * time.Millisecond,
				}, nil)
				if err != nil {
					done <- err
					return
				}
				accepted <- s
				for {
					_, body, err := s.NextReader()
					if err != nil {
						done <- err
						return
					}
					_, err = io.Copy(io.Discard, body)
					closeErr := body.Close()
					if err != nil || closeErr != nil {
						done <- err
						return
					}
				}
			}))
			defer srv.Close()
			c, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			require.NoError(t, err)
			defer func() { _ = c.Close() }()
			s := <-accepted
			defer func() { _ = s.Close() }()
			// Activity lasts longer than the entire idle window, so packets must reset it.
			// Leave enough scheduling margin for coverage and race instrumentation.
			// Drain PONGs independently of the client's packet cadence.
			go func() {
				for {
					if _, _, err := c.ReadMessage(); err != nil {
						return
					}
				}
			}()
			if name != "silent" {
				ticker := time.NewTicker(100 * time.Millisecond)
				defer ticker.Stop()
				for i := 0; i < 20; i++ {
					select {
					case err := <-done:
						t.Fatalf("client closed before its next packet: %v", err)
					case <-ticker.C:
					}
					msg := "2"
					if name == "messages" {
						msg = "4hello"
					}
					require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte(msg)))
				}
				select {
				case err := <-done:
					t.Fatalf("active client closed: %v", err)
				default:
				}
			}
			select {
			case err := <-done:
				require.Error(t, err)
				require.NotNil(t, s.cause.Load())
				require.Equal(t, reasonPingTimeout, s.cause.Load().reason)
			case <-time.After(3 * time.Second):
				t.Fatal("silent client did not time out")
			}
		})
	}
}

// expiringConn returns err from its first NextReader once the read deadline set
// by the session has passed, or at once when the deadline is not awaited, and
// io.EOF from any retry.
type expiringConn struct {
	scriptConn
	err      error
	await    bool
	read     atomic.Bool
	deadline atomic.Value // time.Time
}

func (c *expiringConn) SetReadDeadline(t time.Time) error {
	c.deadline.Store(t)
	return nil
}

func (c *expiringConn) NextReader() (eioparser.FrameType, eioparser.PacketType, io.ReadCloser, error) {
	if c.read.Swap(true) {
		return 0, 0, nil, io.EOF
	}
	if c.await {
		time.Sleep(time.Until(c.deadline.Load().(time.Time)) + 10*time.Millisecond)
	}
	return 0, 0, nil, c.err
}

// timeoutError wraps nothing and reports Timeout, as gorilla's deadline error does.
type timeoutError struct{}

func (timeoutError) Error() string { return "websocket: i/o timeout" }
func (timeoutError) Timeout() bool { return true }

// TestFailClassifiesByDeadlineError checks that the close reason follows the
// transport's error and not the session clock. os.ErrDeadlineExceeded, or an error
// reporting Timeout, closes the session with "ping timeout" even while the clock
// still reads before the stored deadline, as when the transport deadline expires on
// the monotonic clock and the wall clock lags behind it. Any other error is
// "transport error", also once the stored deadline has passed.
func TestFailClassifiesByDeadlineError(t *testing.T) {
	otherErr := errors.New("read: timeout")
	for _, tc := range []struct {
		name  string
		err   error
		await bool
		want  CloseReason
	}{
		{"deadline error before deadline", os.ErrDeadlineExceeded, false, ReasonPingTimeout},
		{"wrapped deadline error", fmt.Errorf("frame: %w", os.ErrDeadlineExceeded), false, ReasonPingTimeout},
		{"timeout error before deadline", timeoutError{}, false, ReasonPingTimeout},
		{"other error before deadline", otherErr, false, ReasonTransportError},
		{"other error after deadline", otherErr, true, ReasonTransportError},
	} {
		t.Run("read "+tc.name, func(t *testing.T) {
			s, err := New(&expiringConn{err: tc.err, await: tc.await}, "sid", "websocket",
				eioparser.ConnParameters{PingInterval: 50 * time.Millisecond, PingTimeout: 50 * time.Millisecond}, nil)
			require.NoError(t, err)
			_, _, err = s.NextReader()
			var closeErr *CloseError
			require.ErrorAs(t, err, &closeErr)
			require.Equal(t, tc.want, closeErr.Reason)
			if tc.want == ReasonTransportError {
				require.ErrorIs(t, closeErr.Err, tc.err)
			}
		})
		if tc.await {
			continue
		}
		t.Run("write "+tc.name, func(t *testing.T) {
			// The stored deadline is PingInterval+PingTimeout = 2 s away.
			s := newTestSession(t, &scriptConn{wCloseErr: tc.err}, "websocket")
			w, err := s.NextWriter(eioparser.String)
			require.NoError(t, err)
			require.ErrorIs(t, w.Close(), tc.err)
			require.NoError(t, s.Close())
			require.Equal(t, tc.want, s.closeError().Reason)
		})
	}
}

// retryWriterConn returns the errs from its successive NextWriter calls.
type retryWriterConn struct {
	scriptConn
	errs []error
}

func (c *retryWriterConn) NextWriter(eioparser.FrameType, eioparser.PacketType) (io.WriteCloser, error) {
	err := c.errs[0]
	c.errs = c.errs[1:]
	return nil, err
}

// TestWriteTimeoutIsNotRetried checks that the session does not retry a
// temporary error that is also a timeout. A retry would surface the next
// error and report "transport error" instead of "ping timeout".
func TestWriteTimeoutIsNotRetried(t *testing.T) {
	timeout := &polling.OpError{Op: "write", Err: os.ErrDeadlineExceeded}
	s := newTestSession(t, &retryWriterConn{errs: []error{timeout, errors.New("broken")}}, "polling")
	_, err := s.NextWriter(eioparser.String)
	require.Error(t, err)
	require.NoError(t, s.Close())
	require.Equal(t, ReasonPingTimeout, s.closeError().Reason)
}
