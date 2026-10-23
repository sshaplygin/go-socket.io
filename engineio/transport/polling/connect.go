package polling

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"

	"github.com/sshaplygin/go-socket.io/engineio/transport"

	"github.com/sshaplygin/go-socket.io/logger"
)

type clientConn struct {
	readLimit atomic.Int64
	*payload

	httpClient   *http.Client
	cancel       context.CancelFunc
	request      http.Request
	remoteHeader atomic.Value
	writableMu   sync.Mutex
	posting      bool
	polling      bool
	started      bool
	stopped      bool
	pausing      bool
	pauseEpoch   uint64
	changed      chan struct{}
}

// SetReadLimit bounds each response body, including OPEN, before decoding.
func (c *clientConn) SetReadLimit(n int64) { c.readLimit.Store(n) }

func (c *clientConn) responseBody(resp *http.Response) io.ReadCloser {
	if n := c.readLimit.Load(); n > 0 {
		return http.MaxBytesReader(nil, resp.Body, n)
	}
	return resp.Body
}

func (c *clientConn) Open() (eioparser.ConnParameters, error) {
	c.writableMu.Lock()
	if c.stopped {
		c.writableMu.Unlock()
		return eioparser.ConnParameters{}, io.ErrClosedPipe
	}
	c.started, c.polling = true, true
	c.writableMu.Unlock()
	// Payload.FeedIn calls must not overlap. getOpen's FeedIn returns only
	// after the whole open response has been read, which can be after Open
	// returns, so serveGet waits for it before its first poll.
	opened := make(chan struct{})
	go func() {
		defer close(opened)
		defer c.setPolling(false)
		c.getOpen()
	}()

	_, pt, r, err := c.NextReader()
	if err != nil {
		return eioparser.ConnParameters{}, err
	}

	if pt != eioparser.OPEN {
		if err = r.Close(); err != nil {
			logger.Log.Warn("engineio: close reader failed", "err", err)
		}

		return eioparser.ConnParameters{}, errors.New("invalid open")
	}

	conn, err := eioparser.ReadConnParameters(r)
	if err != nil {
		if closeErr := r.Close(); closeErr != nil {
			logger.Log.Debug("engineio: read open packet failed", "err", err)
		}

		return eioparser.ConnParameters{}, err
	}

	if err = r.Close(); err != nil {
		return eioparser.ConnParameters{}, err
	}

	query := c.request.URL.Query()
	query.Set("sid", conn.SID)
	c.request.URL.RawQuery = query.Encode()

	go c.serveGet(opened)
	go c.servePost()

	return conn, nil
}

func (c *clientConn) URL() url.URL {
	return *c.request.URL
}

func (c *clientConn) LocalAddr() net.Addr {
	return Addr{""}
}

func (c *clientConn) RemoteAddr() net.Addr {
	return Addr{c.request.Host}
}

func (c *clientConn) RemoteHeader() http.Header {
	ret := c.remoteHeader.Load()
	if ret == nil {
		return nil
	}
	return ret.(http.Header)
}

// Pause gates new GETs and waits for the current response to be consumed and the
// current POST to complete. Engine.IO holds its writers while calling it. Unlike
// Payload.Pause, this wait can be cancelled by Resume without poisoning a payload
// that must remain usable after a failed upgrade.
func (c *clientConn) Pause() { _ = c.PauseContext(context.Background()) }

// PauseContext stops new polls until Resume and waits for current GET/POST work.
// Cancellation also applies before PauseContext starts, so upgrade rollback does
// not depend on when its pause goroutine is scheduled.
func (c *clientConn) PauseContext(ctx context.Context) error {
	c.writableMu.Lock()
	defer c.writableMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.stopped {
		return io.ErrClosedPipe
	}
	c.pausing = true
	c.pauseEpoch++
	epoch := c.pauseEpoch
	c.notifyChanged()
	for c.pausing && c.pauseEpoch == epoch && (c.polling || c.posting) {
		changed := c.changed
		c.writableMu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		c.writableMu.Lock()
		if err := ctx.Err(); err != nil {
			if c.pauseEpoch == epoch {
				c.pausing = false
				c.pauseEpoch++
				c.notifyChanged()
			}
			return err
		}
	}
	if c.stopped {
		return io.ErrClosedPipe
	}
	return nil
}

func (c *clientConn) Resume() {
	c.writableMu.Lock()
	c.pausing = false
	c.pauseEpoch++
	c.notifyChanged()
	start := !c.started && !c.stopped
	c.started = true
	c.writableMu.Unlock()
	if start {
		// Raw transport users can start polling without decoding OPEN.
		c.payload.Resume()
		go c.serveGet(nil)
		go c.servePost()
	}
}

func (c *clientConn) Close() error {
	c.writableMu.Lock()
	c.stopped, c.pausing = true, false
	c.pauseEpoch++
	c.notifyChanged()
	c.writableMu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	return c.payload.Close()
}

func (c *clientConn) servePost() {
	defer c.setPosting(false)
	req := c.request
	reqUrl := *req.URL

	req.URL = &reqUrl
	req.Method = http.MethodPost

	var buf bytes.Buffer
	req.Body = io.NopCloser(&buf)

	query := reqUrl.Query()
	for {
		buf.Reset()

		if err := c.payload.FlushOut(postBodyWriter{Writer: &buf, c: c}); err != nil {
			return
		}
		query.Set("t", transport.Timestamp())
		req.URL.RawQuery = query.Encode()

		resp, err := c.httpClient.Do(&req)
		if err != nil {
			if err = c.payload.Store("post", err); err != nil {
				logger.Log.Debug("engineio: post request failed", "err", err)
			}

			if err = c.Close(); err != nil {
				logger.Log.Debug("engineio: close connection failed", "err", err)
			}

			return
		}

		discardBody(resp.Body)

		if resp.StatusCode != http.StatusOK {
			err = c.payload.Store("post", fmt.Errorf("invalid response: %s(%d)", resp.Status, resp.StatusCode))
			if err != nil {
				logger.Log.Debug("engineio: post request failed", "err", err)
			}

			if err = c.Close(); err != nil {
				logger.Log.Debug("engineio: close connection failed", "err", err)
			}

			return
		}

		c.remoteHeader.Store(resp.Header)
		c.setPosting(false)
	}
}

// Flush waits for the current POST response without waiting for a long-poll GET.
// Writers must be serialized by the caller. Close interrupts this wait.
func (c *clientConn) Flush() error {
	c.writableMu.Lock()
	defer c.writableMu.Unlock()
	for c.posting && !c.stopped {
		changed := c.changed
		c.writableMu.Unlock()
		<-changed
		c.writableMu.Lock()
	}
	if c.stopped {
		return c.payload.load()
	}
	return nil
}

func (c *clientConn) getOpen() {
	req := c.request
	query := req.URL.Query()

	reqUrl := *req.URL
	req.URL = &reqUrl
	req.Method = http.MethodGet

	query.Set("t", transport.Timestamp())
	req.URL.RawQuery = query.Encode()

	resp, err := c.httpClient.Do(&req)
	if err != nil {
		if err = c.payload.Store("get", err); err != nil {
			logger.Log.Debug("engineio: get request failed", "err", err)
		}

		if err = c.Close(); err != nil {
			logger.Log.Debug("engineio: close connection failed", "err", err)
		}

		return
	}

	defer func() {
		discardBody(resp.Body)
	}()

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("invalid request: %s(%d)", resp.Status, resp.StatusCode)
	}

	var isSupportBinary bool
	if err == nil {
		mime := resp.Header.Get("Content-Type")
		isSupportBinary, err = mimeIsSupportBinary(mime)
		if err != nil {
			logger.Log.Debug("engineio: unsupported content type", "err", err)
		}
	}

	if err != nil {
		if err = c.payload.Store("get", err); err != nil {
			logger.Log.Debug("engineio: get request failed", "err", err)
		}

		if err = c.Close(); err != nil {
			logger.Log.Debug("engineio: close connection failed", "err", err)
		}

		return
	}

	c.remoteHeader.Store(resp.Header)

	if err = c.payload.FeedIn(c.responseBody(resp), isSupportBinary); err != nil {
		logger.Log.Debug("engineio: get payload failed", "err", err)

		return
	}
}

// serveGet polls until a request fails or the payload is closed or paused. If
// after is not nil, the first poll is sent after it is closed.
func (c *clientConn) serveGet(after <-chan struct{}) {
	if after != nil {
		<-after
	}

	req := c.request
	reqUrl := *req.URL

	req.URL = &reqUrl
	req.Method = http.MethodGet

	query := req.URL.Query()
	for {
		if !c.beginPoll() {
			return
		}
		query.Set("t", transport.Timestamp())
		req.URL.RawQuery = query.Encode()
		keep := c.poll(&req)
		c.setPolling(false)
		if !keep {
			return
		}
	}
}

func (c *clientConn) poll(req *http.Request) bool {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if err = c.payload.Store("get", err); err != nil {
			logger.Log.Debug("engineio: get request failed", "err", err)
		}

		if err = c.Close(); err != nil {
			logger.Log.Debug("engineio: close connection failed", "err", err)
		}

		return false
	}

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("invalid request: %s(%d)", resp.Status, resp.StatusCode)
	}

	var isSupportBinary bool
	if err == nil {
		mime := resp.Header.Get("Content-Type")
		isSupportBinary, err = mimeIsSupportBinary(mime)
		if err != nil {
			logger.Log.Debug("engineio: unsupported content type", "err", err)
		}
	}

	if err != nil {
		discardBody(resp.Body)

		if err = c.payload.Store("get", err); err != nil {
			logger.Log.Debug("engineio: get request failed", "err", err)
		}

		if err = c.Close(); err != nil {
			logger.Log.Debug("engineio: close connection failed", "err", err)
		}

		return false
	}

	if err = c.payload.FeedIn(c.responseBody(resp), isSupportBinary); err != nil {
		discardBody(resp.Body)

		return false
	}

	c.remoteHeader.Store(resp.Header)
	discardBody(resp.Body)
	return true
}

func discardBody(body io.ReadCloser) {
	_, err := io.Copy(io.Discard, body)
	if err != nil {
		logger.Log.Debug("engineio: discard body failed", "err", err)
	}

	if err = body.Close(); err != nil {
		logger.Log.Debug("engineio: close body failed", "err", err)
	}
}

// Writable reports whether no POST body is being sent or response being read.
func (c *clientConn) Writable() bool {
	c.writableMu.Lock()
	defer c.writableMu.Unlock()
	return !c.posting
}

func (c *clientConn) setPosting(posting bool) {
	c.writableMu.Lock()
	c.posting = posting
	c.notifyChanged()
	c.writableMu.Unlock()
}

func (c *clientConn) setPolling(polling bool) {
	c.writableMu.Lock()
	c.polling = polling
	c.notifyChanged()
	c.writableMu.Unlock()
}

func (c *clientConn) beginPoll() bool {
	c.writableMu.Lock()
	defer c.writableMu.Unlock()
	for c.pausing && !c.stopped {
		changed := c.changed
		c.writableMu.Unlock()
		<-changed
		c.writableMu.Lock()
	}
	if c.stopped {
		return false
	}
	c.polling = true
	return true
}

// notifyChanged is called with writableMu held; waiting callers recheck state.
func (c *clientConn) notifyChanged() {
	if c.changed != nil {
		close(c.changed)
	}
	c.changed = make(chan struct{})
}

type postBodyWriter struct {
	io.Writer
	c *clientConn
}

func (w postBodyWriter) Write(p []byte) (int, error) {
	w.c.setPosting(true)
	return w.Writer.Write(p)
}
