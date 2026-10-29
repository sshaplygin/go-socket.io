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

// closePacketServer accepts one session and sends what its reader returns: each
// message, then the error that ends it.
func closePacketServer(t *testing.T) (string, <-chan interface{}) {
	t.Helper()
	s := engineio.NewServer(engineio.WithPingInterval(time.Hour), engineio.WithPingTimeout(time.Hour),
		engineio.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	read := make(chan interface{}, 2)
	go func() {
		conn, err := s.Accept()
		if err != nil {
			return
		}
		for {
			_, r, err := conn.NextReader()
			if err != nil {
				read <- err
				return
			}
			b, _ := io.ReadAll(r)
			_ = r.Close()
			read <- string(b)
		}
	}()
	return ts.URL, read
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
	for _, want := range []interface{}{"hello", engineio.ReasonTransportClose} {
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
