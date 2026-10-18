package session

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func checkClosed(t *testing.T, s *Session, reason CloseReason, cause error) {
	t.Helper()
	for i := 0; i < 2; i++ {
		_, _, readErr := s.NextReader()
		_, writeErr := s.NextWriter(eioparser.String)
		for _, err := range []error{readErr, writeErr} {
			var closed *CloseError
			require.ErrorAs(t, err, &closed)
			require.Equal(t, reason, closed.Reason)
			require.Equal(t, cause, closed.Err)
			if cause != nil {
				require.ErrorIs(t, err, cause)
			}
			message := "engineio: session closed: " + string(reason)
			if cause != nil {
				message += ": " + cause.Error()
			}
			require.EqualError(t, closed, message)
		}
	}
	require.False(t, s.Writable())
}

// Covers K24
func TestSessionCloseError(t *testing.T) {
	for _, reason := range []CloseReason{ReasonTransportClose, ReasonTransportError, ReasonForcedClose, ReasonServerShutdown, ReasonPingTimeout} {
		t.Run(string(reason), func(t *testing.T) {
			fake := &scriptConn{}
			s := newTestSession(t, fake, "polling")
			var cause error
			switch reason {
			case ReasonTransportClose:
				fake.reads = []scriptedPacket{{ft: eioparser.String, pt: eioparser.CLOSE}}
				cause = io.EOF
			case ReasonTransportError:
				cause = errors.New("reset")
				fake.reads = []scriptedPacket{{err: cause}}
			case ReasonForcedClose:
				require.NoError(t, s.Close())
			case ReasonServerShutdown:
				require.NoError(t, s.Shutdown())
			case ReasonPingTimeout:
				// A real transport deadline, rather than a script exhausting its input.
				accepted := make(chan *Session, 1)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, err := websocket.Default.Accept(w, r)
					if err != nil {
						return
					}
					Session, err := New(conn, "timeout", "websocket", eioparser.ConnParameters{PingInterval: 100 * time.Millisecond, PingTimeout: 50 * time.Millisecond}, nil)
					if err != nil {
						_ = conn.Close()
						return
					}
					accepted <- Session
				}))
				defer srv.Close()
				peer, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
				require.NoError(t, err)
				defer func() { _ = peer.Close() }()
				select {
				case s = <-accepted:
				case <-time.After(time.Second):
					t.Fatal("session not accepted")
				}
			}
			defer func() { _ = s.Close() }()
			checkClosed(t, s, reason, cause)
		})
	}
	t.Run("writer failure closes immediately", func(t *testing.T) {
		cause := errors.New("write reset")
		s := newTestSession(t, &scriptConn{writerErr: cause}, "polling")
		_, err := s.NextWriter(eioparser.String)
		var closed *CloseError
		require.ErrorAs(t, err, &closed)
		require.ErrorIs(t, err, cause)
		checkClosed(t, s, ReasonTransportError, cause)
	})
	t.Run("previous frames keep their own errors", func(t *testing.T) {
		cause := errors.New("frame close failed")
		fake := &scriptConn{reads: []scriptedPacket{{ft: eioparser.String, pt: eioparser.MESSAGE, data: "hello"}}, wCloseErr: cause}
		s := newTestSession(t, fake, "fake")
		_, r, err := s.NextReader()
		require.NoError(t, err)
		w, err := s.NextWriter(eioparser.String)
		require.NoError(t, err)
		require.NoError(t, s.Close())
		data, err := io.ReadAll(r)
		require.NoError(t, err)
		require.Equal(t, "hello", string(data))
		require.NoError(t, r.Close())
		require.ErrorIs(t, w.Close(), cause)
		checkClosed(t, s, ReasonForcedClose, nil)
	})

}

// Covers K16
func TestSessionWritable(t *testing.T) {
	t.Run("optional method and open writer", func(t *testing.T) {
		s := newTestSession(t, &scriptConn{}, "fake")
		require.True(t, s.Writable())
		writer, err := s.NextWriter(eioparser.String)
		require.NoError(t, err)
		require.False(t, s.Writable())
		require.NoError(t, writer.Close())
		require.NoError(t, writer.Close())
		require.True(t, s.Writable())
		require.NoError(t, s.Close())
		require.False(t, s.Writable())
	})
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			accepted := make(chan *Session, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := tr.Accept(w, r)
				if err != nil {
					return
				}
				s := newTestSession(t, conn, tr.Name())
				accepted <- s
				if tr.Name() == "polling" {
					s.ServeHTTP(w, r)
				}
			}))
			defer srv.Close()
			var finished chan struct{}
			if tr.Name() == "polling" {
				finished = make(chan struct{})
				go func() {
					defer close(finished)
					resp, err := http.Get(srv.URL)
					if err == nil {
						_, _ = io.Copy(io.Discard, resp.Body)
						_ = resp.Body.Close()
					}
				}()
			} else {
				peer, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
				require.NoError(t, err)
				defer func() { _ = peer.Close() }()
			}
			var s *Session
			select {
			case s = <-accepted:
			case <-time.After(time.Second):
				t.Fatal("session not accepted")
			}
			defer func() { _ = s.Close() }()
			require.Eventually(t, s.Writable, time.Second, time.Millisecond)
			w, err := s.NextWriter(eioparser.String)
			require.NoError(t, err)
			require.False(t, s.Writable())
			_, err = w.Write([]byte("hello"))
			require.NoError(t, err)
			require.False(t, s.Writable())
			require.NoError(t, w.Close())
			if finished != nil {
				select {
				case <-finished:
				case <-time.After(time.Second):
					t.Fatal("poll did not finish")
				}
				require.False(t, s.Writable(), "no poll is waiting")
			} else {
				require.True(t, s.Writable())
			}
			require.NoError(t, s.Close())
			require.False(t, s.Writable())
		})
	}
}
