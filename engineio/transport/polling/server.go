package polling

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/sshaplygin/go-socket.io/engineio/payload"
	"github.com/sshaplygin/go-socket.io/logger"
)

type serverConn struct {
	*payload.Payload
	transport     *Transport
	supportBinary bool

	remoteHeader http.Header
	localAddr    Addr
	remoteAddr   Addr
	url          url.URL
}

// newServerConn creates the polling connection of a request. JSONP is not supported
// (ROADMAP Stage V1, D4): the `j` parameter is ignored like any unknown parameter.
func newServerConn(t *Transport, r *http.Request) *serverConn {
	supportBinary := r.URL.Query().Get("b64") == ""

	return &serverConn{
		Payload:       payload.New(supportBinary),
		transport:     t,
		supportBinary: supportBinary,
		remoteHeader:  r.Header,
		localAddr:     Addr{r.Host},
		remoteAddr:    Addr{r.RemoteAddr},
		url:           *r.URL,
	}
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

		if c.supportBinary {
			w.Header().Set("Content-Type", "application/octet-stream")
		} else {
			w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		}

		// FlushOut has returned, so the session writer no longer uses w. If it had
		// already started the response, a second one would corrupt it.
		pw := &progressWriter{w: w}
		if err := c.Payload.FlushOut(pw); err != nil {
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

		if err := c.Payload.FeedIn(r.Body, isSupportBinary); err != nil {
			logger.Log.Debug("engineio: post payload failed", "err", err)
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
