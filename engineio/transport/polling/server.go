package polling

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/sshaplygin/go-socket.io/engineio/internal"
	"github.com/sshaplygin/go-socket.io/engineio/payload"
	"github.com/sshaplygin/go-socket.io/logger"
)

type serverConn struct {
	*payload.Payload
	transport  *Transport
	maxPayload int

	closed       atomic.Bool           // Close was called: the session or the server ended it
	flushFail    atomic.Pointer[error] // the first error a poll answered with a 500
	remoteHeader http.Header
	localAddr    Addr
	remoteAddr   Addr
	url          url.URL
}

func newServerConn(t *Transport, r *http.Request) *serverConn {
	maxPayload := t.MaxPayload
	if maxPayload <= 0 {
		maxPayload = payload.DefaultMaxPayload
	}

	// A response is not limited: maxPayload bounds what the server accepts. The
	// Go client reads a response under its own Transport.MaxPayload.
	return &serverConn{
		Payload:      payload.New(maxPayload, 0),
		transport:    t,
		maxPayload:   maxPayload,
		remoteHeader: r.Header,
		localAddr:    Addr{r.Host},
		remoteAddr:   Addr{r.RemoteAddr},
		url:          *r.URL,
	}
}

// Close closes the payload and records that the connection was closed, which
// lowers the log level of the polls that fail afterwards.
func (c *serverConn) Close() error {
	c.closed.Store(true)
	return c.Payload.Close()
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
	w     io.Writer
	wrote bool
}

func (p *progressWriter) Write(b []byte) (int, error) {
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
		w.Header().Set("Content-Type", contentType)

		// FlushOut has returned, so the session writer no longer uses w. If it had
		// already started the response, a second one would corrupt it.
		pw := &progressWriter{w: w}
		if err := c.Payload.FlushOut(pw); err != nil {
			if pw.wrote {
				logger.Log.Debug("engineio: poll response failed after it was started", "err", err)
				return
			}
			c.rejected(r, "flush", c.flushLevel(err), err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

	case http.MethodPost:
		c.SetHeaders(w, r)

		if err := checkContentType(r.Header.Get("Content-Type")); err != nil {
			logger.Log.Debug("engineio: unsupported content type", "err", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// An announced length over the limit is refused before a byte is read; an
		// unannounced or understated one is cut by the reader at the limit.
		if r.ContentLength > int64(c.maxPayload) {
			http.Error(w, payload.ErrTooLarge.Error(), http.StatusRequestEntityTooLarge)
			return
		}

		// The session's read deadline also bounds the body read.
		if d := c.Payload.ReadDeadline(); !d.IsZero() {
			if err := http.NewResponseController(w).SetReadDeadline(d); err != nil && !errors.Is(err, http.ErrNotSupported) {
				logger.Log.Debug("engineio: set body read deadline failed", "err", err)
			}
		}

		if err := c.Payload.FeedIn(r.Body); err != nil {
			logger.Log.Debug("engineio: post payload failed", "err", err)
			http.Error(w, err.Error(), postStatus(err))
			return
		}

		_, err := w.Write([]byte("ok"))
		if err != nil {
			logger.Log.Debug("engineio: post answer failed", "err", err)
		}

	default:
		c.rejected(r, "bad method", slog.LevelWarn, nil)
		internal.WriteError(w, http.StatusBadRequest, internal.CodeBadRequest)
	}
}

// flushLevel is the level at which a poll that failed with err is logged: DEBUG when
// the connection was closed or the payload had failed with the same error before,
// WARN for the failure that first ends the payload and for anything else.
func (c *serverConn) flushLevel(err error) slog.Level {
	if c.closed.Load() || errors.Is(err, io.EOF) {
		return slog.LevelDebug
	}
	if !c.flushFail.CompareAndSwap(nil, &err) && errors.Is(err, *c.flushFail.Load()) {
		return slog.LevelDebug
	}
	return slog.LevelWarn
}

// rejected logs a request answered with an error status through the fallback logger.
func (c *serverConn) rejected(r *http.Request, reason string, level slog.Level, err error) {
	args := []any{"transport", "polling", "remote_addr", r.RemoteAddr, "reason", reason}
	if sid := c.url.Query().Get("sid"); sid != "" {
		args = append(args, "sid", sid)
	}
	if err != nil {
		args = append(args, "err", err)
	}
	logger.Log.Log(context.Background(), level, "engineio: request rejected", args...)
}

// postStatus maps an error of FeedIn to the status of the POST it answers.
func postStatus(err error) int {
	if errors.Is(err, payload.ErrTooLarge) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}
