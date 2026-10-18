package polling

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"

	"github.com/sshaplygin/go-socket.io/logger"
)

type serverConn struct {
	*payload
	transport     *Transport
	supportBinary bool

	remoteHeader http.Header
	localAddr    Addr
	remoteAddr   Addr
	url          url.URL
	readLimit    int64
	writableMu   sync.Mutex
	pollWaiting  bool
	noCompress   atomic.Bool
	cookie       atomic.Pointer[http.Cookie]
}

// newServerConn creates the polling connection of a request. JSONP is not supported
// (ROADMAP Stage V1, D4): the `j` parameter is ignored like any unknown parameter.
func newServerConn(t *Transport, r *http.Request) *serverConn {
	supportBinary := r.URL.Query().Get("b64") == ""

	return &serverConn{
		payload:       newPayload(supportBinary),
		transport:     t,
		supportBinary: supportBinary,
		remoteHeader:  r.Header,
		localAddr:     Addr{r.Host},
		remoteAddr:    Addr{r.RemoteAddr},
		url:           *r.URL,
		readLimit:     1_000_000,
	}
}

// SetReadLimit caps a POST body. An excess is answered with HTTP 413 and fails
// the session's reader with a transport error.
func (c *serverConn) SetReadLimit(n int64) { c.readLimit = n }

// CloseGracefully writes CLOSE through the next available poll. The session
// serializes it after admitted frames and bounds the wait; Close interrupts it.
func (c *serverConn) CloseGracefully() error {
	w, err := c.NextWriter(eioparser.String, eioparser.CLOSE)
	if err != nil {
		return err
	}
	return w.Close()
}

func (c *serverConn) URL() url.URL {
	return c.url
}

func (c *serverConn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *serverConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *serverConn) RemoteHeader() http.Header {
	return c.remoteHeader
}

func (c *serverConn) SetHeaders(w http.ResponseWriter, r *http.Request) {
	if cookie := c.cookie.Load(); cookie != nil {
		http.SetCookie(w, cookie)
	}
	if strings.Contains(r.UserAgent(), ";MSIE") || strings.Contains(r.UserAgent(), "Trident/") {
		w.Header().Set("X-XSS-Protection", "0")
	}

	// just in case the default behaviour gets changed and it has to handle an origin check
	checkOrigin := Default.CheckOrigin
	if c.transport.CheckOrigin != nil {
		checkOrigin = c.transport.CheckOrigin
	}

	if checkOrigin != nil && checkOrigin(r) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
	}
}

// progressWriter records whether a write to w was attempted.
type progressWriter struct {
	w       io.Writer
	wrote   bool
	started func()
}

func (p *progressWriter) Write(b []byte) (int, error) {
	if !p.wrote && p.started != nil {
		p.started()
	}
	p.wrote = true
	return p.w.Write(b)
}

func (c *serverConn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodOptions:
		c.SetHeaders(w, r)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(200)

	case http.MethodGet:
		c.SetHeaders(w, r)

		if c.supportBinary {
			w.Header().Set("Content-Type", "application/octet-stream")
		} else {
			w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		}

		// FlushOut has returned, so the session writer no longer uses w. If it had
		// already started the response, a second one would corrupt it.
		c.setPollWaiting(true)
		defer c.setPollWaiting(false)
		var out io.Writer = w
		if c.transport.Compression {
			w.Header().Add("Vary", "Accept-Encoding")
			out = &responseWriter{w: w, conn: c, encoding: acceptedEncoding(r.Header.Get("Accept-Encoding"))}
		}
		pw := &progressWriter{w: out, started: func() { c.setPollWaiting(false) }}
		if err := c.payload.FlushOut(pw); err != nil {
			if pw.wrote {
				logger.Log.Debug("engineio: poll response failed after it was started", "err", err)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case http.MethodPost:
		c.SetHeaders(w, r)

		mime := r.Header.Get("Content-Type")
		isSupportBinary, err := mimeIsSupportBinary(mime)
		if err != nil {
			logger.Log.Debug("engineio: unsupported content type", "err", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		body := http.MaxBytesReader(w, r.Body, c.readLimit)
		defer func() { _ = body.Close() }()
		if r.ContentLength > c.readLimit {
			c.rejectLargeBody(w, &http.MaxBytesError{Limit: c.readLimit})
			return
		}
		if err := c.payload.FeedIn(body, isSupportBinary); err != nil {
			logger.Log.Debug("engineio: post payload failed", "err", err)
			cause := err
			if op, ok := cause.(*OpError); ok {
				cause = op.Err
			}
			var limitErr *http.MaxBytesError
			if errors.As(cause, &limitErr) {
				c.rejectLargeBody(w, limitErr)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_, err = w.Write([]byte("ok"))
		if err != nil {
			logger.Log.Debug("engineio: post answer failed", "err", err)
		}

	default:
		http.Error(w, "invalid method", http.StatusBadRequest)
	}
}

func (c *serverConn) rejectLargeBody(w http.ResponseWriter, err *http.MaxBytesError) {
	_ = c.payload.Store("read", err)
	_ = c.Close()
	http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
}

// Writable reports whether a poll is waiting and its response has not started.
func (c *serverConn) Writable() bool {
	c.writableMu.Lock()
	defer c.writableMu.Unlock()
	return c.pollWaiting
}

func (c *serverConn) setPollWaiting(waiting bool) {
	c.writableMu.Lock()
	c.pollWaiting = waiting
	c.writableMu.Unlock()
}
