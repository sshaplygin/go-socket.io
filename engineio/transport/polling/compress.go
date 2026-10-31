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
	c.noCompress.Store(!compress)
	c.payload.compress.Store(compress)
	return c.payload.NextWriter(ft, pt)
}

// SetSessionID installs a private cookie before the handshake response.
func (c *serverConn) SetSessionID(id string) {
	if c.transport.Cookie != nil {
		cookie := *c.transport.Cookie
		cookie.Value = id
		c.cookie.Store(&cookie)
	}
}

// responseWriter chooses the HTTP encoding from the first complete packet and
// keeps one compression stream for the entire batch, flushed after each packet
// write. It writes to the response buffer that the GET handler then sends.
type responseWriter struct {
	w          http.ResponseWriter
	conn       *serverConn
	encoding   string
	prefix     []byte
	out        io.Writer
	compressor interface {
		io.WriteCloser
		Flush() error
	}
}

func (w *responseWriter) Write(p []byte) (int, error) {
	if w.out == nil {
		if w.prefix == nil && !completeHeader(p) {
			w.prefix = bytes.Clone(p)
			return len(p), nil
		}
		compress := !w.conn.noCompress.Load()
		if w.prefix == nil && (bytes.Equal(p, []byte("1:6")) || bytes.Equal(p, []byte{0, 1, 255, '6'})) {
			compress = true
		}
		threshold := w.conn.transport.CompressionThreshold
		if threshold <= 0 {
			threshold = 1024
		}
		w.out = w.w
		if compress && len(w.prefix)+len(p) >= threshold && w.encoding != "" {
			w.w.Header().Set("Content-Encoding", w.encoding)
			w.w.Header().Del("Content-Length")
			if w.encoding == "gzip" {
				w.compressor = gzip.NewWriter(w.w)
			} else {
				w.compressor = zlib.NewWriter(w.w)
			}
			w.out = w.compressor
		}
		if len(w.prefix) != 0 {
			if _, err := w.out.Write(w.prefix); err != nil {
				return 0, err
			}
			w.prefix = nil
		}
	}
	n, err := w.out.Write(p)
	if err == nil && w.compressor != nil {
		err = w.compressor.Flush()
	}
	return n, err
}

func (w *responseWriter) Close() error {
	if w.compressor != nil {
		return w.compressor.Close()
	}
	return nil
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
