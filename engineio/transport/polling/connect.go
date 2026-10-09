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
	"sync/atomic"

	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/payload"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/utils"
	"github.com/sshaplygin/go-socket.io/logger"
)

type clientConn struct {
	*payload.Payload

	httpClient   *http.Client
	request      http.Request
	remoteHeader atomic.Value

	// cancel aborts the requests in flight, such as a long poll, once the
	// connection is closed.
	ctx    context.Context
	cancel context.CancelFunc
}

// storeUnlessClosed stores the failure of a request, unless Close aborted it:
// the reader and writer then report the close, io.EOF, not the abort.
func (c *clientConn) storeUnlessClosed(op string, err error) error {
	if c.ctx.Err() != nil {
		return nil
	}
	return c.Payload.Store(op, err)
}

// Close closes the payload and aborts the requests in flight.
func (c *clientConn) Close() error {
	err := c.Payload.Close()
	c.cancel()
	return err
}

func (c *clientConn) Open() (transport.ConnParameters, error) {
	// Payload.FeedIn calls must not overlap. getOpen's FeedIn returns only
	// after the whole open response has been read, which can be after Open
	// returns, so serveGet waits for it before its first poll.
	opened := make(chan struct{})
	go func() {
		defer close(opened)
		c.getOpen()
	}()

	_, pt, r, err := c.NextReader()
	if err != nil {
		return transport.ConnParameters{}, err
	}

	if pt != packet.OPEN {
		if err = r.Close(); err != nil {
			logger.Log.Warn("engineio: close reader failed", "err", err)
		}

		return transport.ConnParameters{}, errors.New("invalid open")
	}

	conn, err := transport.ReadConnParameters(r)
	if err != nil {
		if closeErr := r.Close(); closeErr != nil {
			logger.Log.Debug("engineio: read open packet failed", "err", err)
		}

		return transport.ConnParameters{}, err
	}

	if err = r.Close(); err != nil {
		return transport.ConnParameters{}, err
	}

	if conn.MaxPayload > 0 {
		c.Payload.SetWriteLimit(conn.MaxPayload)
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

func (c *clientConn) Resume() {
	c.Payload.Resume()

	go c.serveGet(nil)
	go c.servePost()
}

func (c *clientConn) servePost() {
	req := c.request
	reqUrl := *req.URL

	req.URL = &reqUrl
	req.Method = http.MethodPost

	var buf bytes.Buffer
	req.Body = io.NopCloser(&buf)

	query := reqUrl.Query()
	for {
		buf.Reset()

		if err := c.Payload.FlushOut(&buf); err != nil {
			return
		}
		query.Set("t", utils.Timestamp())
		req.URL.RawQuery = query.Encode()
		req.ContentLength = int64(buf.Len())

		resp, err := c.httpClient.Do(&req)
		if err != nil {
			if err = c.storeUnlessClosed("post", err); err != nil {
				logger.Log.Debug("engineio: post request failed", "err", err)
			}

			if err = c.Close(); err != nil {
				logger.Log.Debug("engineio: close connection failed", "err", err)
			}

			return
		}

		discardBody(resp.Body)

		if resp.StatusCode != http.StatusOK {
			err = c.Payload.Store("post", fmt.Errorf("invalid response: %s(%d)", resp.Status, resp.StatusCode))
			if err != nil {
				logger.Log.Debug("engineio: post request failed", "err", err)
			}

			if err = c.Close(); err != nil {
				logger.Log.Debug("engineio: close connection failed", "err", err)
			}

			return
		}

		c.remoteHeader.Store(resp.Header)
	}
}

func (c *clientConn) getOpen() {
	req := c.request
	query := req.URL.Query()

	reqUrl := *req.URL
	req.URL = &reqUrl
	req.Method = http.MethodGet

	query.Set("t", utils.Timestamp())
	req.URL.RawQuery = query.Encode()

	resp, err := c.httpClient.Do(&req)
	if err != nil {
		if err = c.storeUnlessClosed("get", err); err != nil {
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

	if err == nil {
		err = checkContentType(resp.Header.Get("Content-Type"))
		if err != nil {
			logger.Log.Debug("engineio: unsupported content type", "err", err)
		}
	}

	if err != nil {
		if err = c.storeUnlessClosed("get", err); err != nil {
			logger.Log.Debug("engineio: get request failed", "err", err)
		}

		if err = c.Close(); err != nil {
			logger.Log.Debug("engineio: close connection failed", "err", err)
		}

		return
	}

	c.remoteHeader.Store(resp.Header)

	if err = c.Payload.FeedIn(resp.Body); err != nil {
		logger.Log.Debug("engineio: get payload failed", "err", err)
		c.failOversized(err)

		return
	}
}

// failOversized ends the session when a response was over the read limit: the
// server does not limit its responses, so nothing else would tell the reader that
// polling stopped. Any other FeedIn error is a close or a pause, which the payload
// reports itself, or a failure it has already stored.
func (c *clientConn) failOversized(err error) {
	if !errors.Is(err, payload.ErrTooLarge) {
		return
	}

	if err = c.storeUnlessClosed("get", err); err != nil {
		logger.Log.Debug("engineio: get response too large", "err", err)
	}

	if err = c.Close(); err != nil {
		logger.Log.Debug("engineio: close connection failed", "err", err)
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
		query.Set("t", utils.Timestamp())
		req.URL.RawQuery = query.Encode()

		resp, err := c.httpClient.Do(&req)
		if err != nil {
			if err = c.storeUnlessClosed("get", err); err != nil {
				logger.Log.Debug("engineio: get request failed", "err", err)
			}

			if err = c.Close(); err != nil {
				logger.Log.Debug("engineio: close connection failed", "err", err)
			}

			return
		}

		if resp.StatusCode != http.StatusOK {
			err = fmt.Errorf("invalid request: %s(%d)", resp.Status, resp.StatusCode)
		}

		if err == nil {
			err = checkContentType(resp.Header.Get("Content-Type"))
			if err != nil {
				logger.Log.Debug("engineio: unsupported content type", "err", err)
			}
		}

		if err != nil {
			discardBody(resp.Body)

			if err = c.Payload.Store("get", err); err != nil {
				logger.Log.Debug("engineio: get request failed", "err", err)
			}

			if err = c.Close(); err != nil {
				logger.Log.Debug("engineio: close connection failed", "err", err)
			}

			return
		}

		if err = c.Payload.FeedIn(resp.Body); err != nil {
			discardBody(resp.Body)
			c.failOversized(err)

			return
		}

		c.remoteHeader.Store(resp.Header)
	}
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
