package polling

import (
	"bytes"
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

		resp, err := c.httpClient.Do(&req)
		if err != nil {
			if err = c.Payload.Store("post", err); err != nil {
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
		if err = c.Payload.Store("get", err); err != nil {
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
		if err = c.Payload.Store("get", err); err != nil {
			logger.Log.Debug("engineio: get request failed", "err", err)
		}

		if err = c.Close(); err != nil {
			logger.Log.Debug("engineio: close connection failed", "err", err)
		}

		return
	}

	c.remoteHeader.Store(resp.Header)

	if err = c.Payload.FeedIn(resp.Body, isSupportBinary); err != nil {
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
		query.Set("t", utils.Timestamp())
		req.URL.RawQuery = query.Encode()

		resp, err := c.httpClient.Do(&req)
		if err != nil {
			if err = c.Payload.Store("get", err); err != nil {
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

			if err = c.Payload.Store("get", err); err != nil {
				logger.Log.Debug("engineio: get request failed", "err", err)
			}

			if err = c.Close(); err != nil {
				logger.Log.Debug("engineio: close connection failed", "err", err)
			}

			return
		}

		if err = c.Payload.FeedIn(resp.Body, isSupportBinary); err != nil {
			discardBody(resp.Body)

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
