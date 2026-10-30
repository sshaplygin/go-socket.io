package client

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// postHook passes each POST body to hook, which may hold the request, then
// forwards the request unless hook returns an error.
func postHook(hook func(r *http.Request, body []byte) error) *polling.Transport {
	return &polling.Transport{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.Body != nil {
			body, err := io.ReadAll(r.Body)
			if err == nil {
				err = hook(r, body)
			}
			if err != nil {
				return nil, err
			}
			r = r.Clone(r.Context())
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		return http.DefaultTransport.RoundTrip(r)
	})}}
}

var closePacketBody = []byte{0x00, 0x01, 0xff, '1'} // CLOSE in a binary payload

// closePacketServer accepts sessions one after another and sends what each
// reader returns: every message, then the error that ends the session.
func closePacketServer(t *testing.T) (string, <-chan interface{}) {
	t.Helper()
	s := engineio.NewServer(engineio.WithPingInterval(time.Hour), engineio.WithPingTimeout(time.Hour),
		engineio.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	read := make(chan interface{}, 64)
	go func() {
		for {
			conn, err := s.Accept()
			if err != nil {
				return
			}
			for {
				_, r, err := conn.NextReader()
				if err != nil {
					read <- err
					break
				}
				b, _ := io.ReadAll(r)
				_ = r.Close()
				read <- string(b)
			}
		}
	}()
	return ts.URL, read
}

// expectRead fails unless the server reads want next: a message, or the
// engineio.CloseError reason that ends the session.
func expectRead(t *testing.T, read <-chan interface{}, want interface{}) {
	t.Helper()
	select {
	case got := <-read:
		if err, ok := got.(error); ok {
			var closed *engineio.CloseError
			require.True(t, errors.As(err, &closed), "%v", err)
			got = closed.Reason
		}
		require.Equal(t, want, got)
	case <-time.After(3 * time.Second):
		t.Fatalf("server did not read %v; without CLOSE the session lasts PingInterval+PingTimeout", want)
	}
}

func TestPollingCloseSendsClosePacketAfterInflightPost(t *testing.T) {
	url, read := closePacketServer(t)
	tr := postHook(func(_ *http.Request, body []byte) error {
		if bytes.Contains(body, []byte("hello")) {
			time.Sleep(200 * time.Millisecond) // Close starts while this POST is in flight
		}
		return nil
	})
	c, err := (&Dialer{Transports: []transport.Transport{tr}}).Dial(url, nil)
	require.NoError(t, err)
	w, err := c.NextWriter(eioparser.String)
	require.NoError(t, err)
	_, err = w.Write([]byte("hello"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, c.Close())
	expectRead(t, read, "hello")
	expectRead(t, read, engineio.ReasonTransportClose)
}

// TestPollingCloseQueuesCloseBehindAdmittedWriter: a writer holds the write
// gate, so its frame is admitted, but has not reached the transport when Close
// is called while a POST is in flight. Close must queue CLOSE behind that
// frame. A writer already waiting in the transport is served first in any case,
// so the gate is taken directly, as NextWriter does before it reaches the
// transport. Each round dials afresh.
func TestPollingCloseQueuesCloseBehindAdmittedWriter(t *testing.T) {
	url, read := closePacketServer(t)
	for round := 0; round < 20; round++ {
		entered, release := make(chan struct{}), make(chan struct{})
		tr := postHook(func(_ *http.Request, body []byte) error {
			if bytes.Contains(body, []byte("first")) {
				close(entered)
				<-release
			}
			return nil
		})
		c, err := (&Dialer{Transports: []transport.Transport{tr}}).Dial(url, nil)
		require.NoError(t, err)
		w, err := c.NextWriter(eioparser.String)
		require.NoError(t, err)
		_, err = w.Write([]byte("first"))
		require.NoError(t, err)
		require.NoError(t, w.Close())
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("no POST of the first message")
		}
		cl := c.(*client)
		require.NoError(t, cl.acquireWrite(nil)) // an admitted writer
		require.NoError(t, c.Close())
		close(release)
		expectRead(t, read, "first") // the in-flight POST completed; CLOSE is not in the next one
		w, err = cl.connection().NextWriter(eioparser.String, eioparser.MESSAGE)
		require.NoError(t, err)
		_, err = w.Write([]byte("second"))
		require.NoError(t, err)
		require.NoError(t, w.Close())
		cl.releaseWrite()
		expectRead(t, read, "second")
		expectRead(t, read, engineio.ReasonTransportClose)
	}
}

func TestPollingCloseUnansweredClosePacket(t *testing.T) {
	url, _ := closePacketServer(t)
	entered, canceled := make(chan struct{}), make(chan struct{})
	tr := postHook(func(r *http.Request, body []byte) error {
		if !bytes.Equal(body, closePacketBody) {
			return nil
		}
		close(entered)
		<-r.Context().Done() // the server never answers
		close(canceled)
		return r.Context().Err()
	})
	c, err := (&Dialer{Transports: []transport.Transport{tr}}).Dial(url, nil)
	require.NoError(t, err)
	started := time.Now()
	require.NoError(t, c.Close())
	require.Less(t, time.Since(started), 100*time.Millisecond, "Close waited for the CLOSE POST")
	require.NoError(t, c.Close())
	require.Equal(t, StateClosed, c.ReadyState())
	for _, step := range []struct {
		ch   <-chan struct{}
		what string
	}{{entered, "CLOSE packet POST"}, {canceled, "cancellation of the unanswered CLOSE POST"}} {
		select {
		case <-step.ch:
		case <-time.After(3 * time.Second):
			t.Fatalf("no %s", step.what)
		}
	}
	_, _, err = c.NextReader()
	var closed *CloseError
	require.True(t, errors.As(err, &closed), "%v", err)
	require.Equal(t, ReasonForcedClose, closed.Reason)
}
