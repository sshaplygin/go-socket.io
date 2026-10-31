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
	"time"

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

	// pollMu guards poll, held by a GET handler from its flushOut to the end of its
	// response write (another GET is an overlap), and pollDone, closed when it returns.
	pollMu   sync.Mutex
	poll     bool
	pollDone chan struct{}

	// ioMu orders deadline updates and Close against tracked request I/O. It is
	// a leaf lock: only ResponseController deadline setters run under it.
	ioMu      sync.Mutex
	ioClosed  bool       // Close ran: body reads are expired
	ioAborted bool       // Abort ran: response writes are expired too
	reading   requestSet // the POSTs whose bodies are being read
	writing   requestSet // the GETs whose responses are being written
}

// newServerConn creates the polling connection of a request. JSONP is not supported
//
//	the `j` parameter is ignored like any unknown parameter.
func newServerConn(t *Transport, r *http.Request) *serverConn {
	supportBinary := r.URL.Query().Get("b64") == ""

	p := newPayload(supportBinary)
	p.batch = true
	return &serverConn{
		payload:       p,
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
	start := time.Now()
	switch r.Method {
	case http.MethodOptions:
		c.SetHeaders(w, r)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(200)

	case http.MethodGet:
		c.SetHeaders(w, r)
		if !c.beginPoll() {
			http.Error(w, newOpError("write", errOverlap).Error(), http.StatusInternalServerError)
			return
		}
		defer c.endPoll()

		if c.supportBinary {
			w.Header().Set("Content-Type", "application/octet-stream")
		} else {
			w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		}

		// The session writer fills resp; FlushOut has returned once this
		// goroutine writes it. If the response had started, a second one would
		// corrupt it.
		c.setPollWaiting(true)
		defer c.setPollWaiting(false)
		resp := &responseBuffer{ResponseWriter: w}
		var out io.Writer = resp
		var compressed *responseWriter
		if c.transport.Compression {
			w.Header().Add("Vary", "Accept-Encoding")
			compressed = &responseWriter{w: resp, conn: c, encoding: acceptedEncoding(r.Header.Get("Accept-Encoding"))}
			out = compressed
		}
		pw := &progressWriter{w: out, started: func() { c.setPollWaiting(false) }}
		err := c.payload.flushOut(r.Context(), pw)
		if compressed != nil {
			if err := compressed.Close(); err != nil {
				_ = c.payload.Store("write", err)
				_ = c.Close()
			}
		}
		if pw.wrote {
			if werr := c.writeResponse(w, r, start, resp.buf.Bytes()); err == nil {
				err = werr
			}
			if err != nil {
				logger.Log.Debug("engineio: poll response failed after it was started", "err", err)
			}
			return
		}
		if err != nil {
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
		if r.ContentLength > c.readLimit {
			c.rejectLargeBody(w, &http.MaxBytesError{Limit: c.readLimit})
			return
		}
		if err := c.feedBody(w, r, start, body, isSupportBinary); err != nil {
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

// beginPoll admits the connection's one GET handler at a time.
func (c *serverConn) beginPoll() bool {
	c.pollMu.Lock()
	defer c.pollMu.Unlock()
	if c.poll {
		return false
	}
	c.poll, c.pollDone = true, make(chan struct{})
	return true
}

func (c *serverConn) endPoll() {
	c.pollMu.Lock()
	c.poll = false
	close(c.pollDone)
	c.pollMu.Unlock()
}

// Drained closes when the GET handler of the moment has returned, so after Close
// its response write has ended; it is closed already when no GET is being handled.
func (c *serverConn) Drained() <-chan struct{} {
	c.pollMu.Lock()
	defer c.pollMu.Unlock()
	if c.poll {
		return c.pollDone
	}
	return closedChan
}

var closedChan = func() chan struct{} { ch := make(chan struct{}); close(ch); return ch }()

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
