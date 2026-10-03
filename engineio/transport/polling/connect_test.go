package polling

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/googollee/go-socket.io/engineio/frame"
	"github.com/googollee/go-socket.io/engineio/packet"
	"github.com/googollee/go-socket.io/engineio/transport"
)

func TestDialOpen(t *testing.T) {
	for _, delayed := range []bool{false, true} {
		name := "POST active"
		if delayed {
			name = "POST starts after writer closes"
		}
		t.Run(name, func(t *testing.T) { testDialOpen(t, delayed) })
	}
}

// dialOpenRoundTripper lets the test control when the serialized POST is sent.
type dialOpenRoundTripper func(*http.Request) (*http.Response, error)

func (f dialOpenRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testDialOpen(t *testing.T, delayedPOST bool) {
	should := assert.New(t)
	must := require.New(t)

	cp := transport.ConnParameters{
		PingInterval: time.Second,
		PingTimeout:  time.Minute,
		SID:          "abcdefg",
		Upgrades:     []string{"polling"},
	}

	buf := bytes.NewBuffer(nil)
	_, err := cp.WriteTo(buf)
	must.NoError(err)

	// HTTP handlers return their errors to the test goroutine. In particular,
	// a failed POST read must still signal completion instead of calling Fatal.
	type postResult struct {
		sid  string
		body []byte
		err  error
	}
	posted := make(chan postResult, 1)
	var handlerMu sync.Mutex
	var handlerErrors []error
	recordHandlerError := func(err error) {
		handlerMu.Lock()
		defer handlerMu.Unlock()
		handlerErrors = append(handlerErrors, err)
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		query := r.URL.Query()
		if query.Get("t") == "" {
			recordHandlerError(fmt.Errorf("%s request has no timestamp", r.Method))
		}
		sid := query.Get("sid")
		if sid == "" {
			if _, err := fmt.Fprintf(w, "%d:0%s", buf.Len()+1, buf.String()); err != nil {
				recordHandlerError(fmt.Errorf("write OPEN: %w", err))
			}
			return
		}
		if r.Method == http.MethodPost {
			b, err := io.ReadAll(r.Body)
			posted <- postResult{sid: sid, body: b, err: err}
		}
	}

	httpSvr := httptest.NewServer(http.HandlerFunc(handler))
	defer func() {
		httpSvr.Close()
		handlerMu.Lock()
		defer handlerMu.Unlock()
		for _, err := range handlerErrors {
			should.NoError(err)
		}
	}()

	u, err := url.Parse(httpSvr.URL)
	must.NoError(err)

	query := u.Query()
	query.Set("b64", "1")
	u.RawQuery = query.Encode()

	postGate := make(chan struct{})
	var releaseOnce sync.Once
	releasePOST := func() { releaseOnce.Do(func() { close(postGate) }) }
	defer releasePOST()
	if !delayedPOST {
		releasePOST()
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: dialOpenRoundTripper(func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodPost {
				select {
				case <-postGate:
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			}
			return http.DefaultTransport.RoundTrip(r)
		}),
	}
	cc, err := dial(client, u, nil)
	must.NoError(err)

	defer func() {
		must.NoError(cc.Close())
	}()

	params, err := cc.Open()
	must.NoError(err)

	should.Equal(cp, params)

	ccURL := cc.URL()
	sid := ccURL.Query().Get("sid")

	should.Equal(cp.SID, sid)

	w, err := cc.NextWriter(frame.String, packet.MESSAGE)
	must.NoError(err)

	_, err = w.Write([]byte("hello"))
	must.NoError(err)
	must.NoError(w.Close())

	// Closing the writer only finishes FlushOut into servePost's buffer. The
	// HTTP request and server body read can both still be pending at this point.
	releasePOST()
	select {
	case result := <-posted:
		must.NoError(result.err)
		should.Equal(cp.SID, result.sid)
		should.Equal("6:4hello", string(result.body))
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for polling POST body before teardown")
	}
}
