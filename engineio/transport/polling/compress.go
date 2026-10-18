package polling

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"strconv"
	"strings"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// NextWriter sends packets with the reference's default compression flag.
func (c *serverConn) NextWriter(ft eioparser.FrameType, pt eioparser.PacketType) (io.WriteCloser, error) {
	return c.NextWriterCompress(ft, pt, true)
}

// NextWriterCompress carries the flag until the payload encoder writes its frame.
func (c *serverConn) NextWriterCompress(ft eioparser.FrameType, pt eioparser.PacketType, compress bool) (io.WriteCloser, error) {
	w, err := c.payload.NextWriter(ft, pt)
	if err != nil {
		return nil, err
	}
	return flaggedWriter{WriteCloser: w, conn: c, compress: compress}, nil
}

type flaggedWriter struct {
	io.WriteCloser
	conn     *serverConn
	compress bool
}

func (w flaggedWriter) Close() error {
	w.conn.noCompress.Store(!w.compress)
	return w.WriteCloser.Close()
}

// SetSessionID installs a private cookie before the handshake response.
func (c *serverConn) SetSessionID(id string) {
	if c.transport.Cookie != nil {
		cookie := *c.transport.Cookie
		cookie.Value = id
		c.cookie.Store(&cookie)
	}
}

// The payload encoder writes one length/type header and one body per response;
// an empty frame (and NOOP) consists of the header alone. Keep only that short
// header until the body length is known. Network errors still reach the payload
// writer synchronously, before FlushOut reports completion.
type responseWriter struct {
	w        http.ResponseWriter
	conn     *serverConn
	encoding string
	prefix   []byte
}

func (w *responseWriter) Write(p []byte) (int, error) {
	if w.prefix == nil && !completeHeader(p) {
		w.prefix = bytes.Clone(p)
		return len(p), nil
	}
	compress := !w.conn.noCompress.Load()
	if w.prefix == nil && (bytes.Equal(p, []byte("1:6")) || bytes.Equal(p, []byte{0, 1, 255, '6'})) {
		compress = true // transport-generated NOOP has the default flag
	}
	threshold := w.conn.transport.CompressionThreshold
	if threshold <= 0 {
		threshold = 1024
	}
	var out io.Writer = w.w
	var closer io.Closer
	if compress && len(w.prefix)+len(p) >= threshold && w.encoding != "" {
		w.w.Header().Set("Content-Encoding", w.encoding)
		w.w.Header().Del("Content-Length")
		if w.encoding == "gzip" {
			out = gzip.NewWriter(w.w)
		} else {
			out = zlib.NewWriter(w.w)
		}
		closer = out.(io.Closer)
	}
	var err error
	if len(w.prefix) != 0 {
		_, err = out.Write(w.prefix)
	}
	var n int
	if err == nil {
		n, err = out.Write(p)
	}
	if closer != nil {
		if closeErr := closer.Close(); err == nil {
			err = closeErr
		}
	}
	return n, err
}

func completeHeader(p []byte) bool {
	// Binary payload lengths use decimal digit bytes followed by 0xff.
	if len(p) > 0 && p[0] <= 1 {
		n := 0
		for i := 1; i < len(p); i++ {
			if p[i] == 255 {
				return n == len(p)-i-1
			}
			n = n*10 + int(p[i])
		}
		return false
	}
	i := bytes.IndexByte(p, ':')
	if i < 0 {
		return false
	}
	n, err := strconv.Atoi(string(p[:i]))
	return err == nil && n == len(p)-i-1
}

func acceptedEncoding(header string) string {
	qualities := map[string]float64{}
	for _, item := range strings.Split(header, ",") {
		parts := strings.Split(item, ";")
		name, q := strings.ToLower(strings.TrimSpace(parts[0])), 1.0
		for _, parameter := range parts[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(key, "q") {
				parsed, err := strconv.ParseFloat(value, 64)
				if err != nil || parsed < 0 || parsed > 1 {
					q = 0
				} else {
					q = parsed
				}
			}
		}
		qualities[name] = q
	}
	best, bestQ := "", 0.0
	for _, name := range []string{"gzip", "deflate"} {
		q, exists := qualities[name]
		if !exists {
			q = qualities["*"]
		}
		if q > bestQ {
			best, bestQ = name, q
		}
	}
	return best
}
