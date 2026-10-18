package engineio

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"

	gorilla "github.com/gorilla/websocket"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/session"

	"github.com/stretchr/testify/require"
)

type compressionPeer struct {
	net.Conn
	reader   *bufio.Reader
	response *http.Response
}

func compressionDial(t *testing.T, target string) *compressionPeer {
	t.Helper()
	c, err := net.DialTimeout("tcp", strings.TrimPrefix(target, "http://"), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.SetDeadline(time.Now().Add(3*time.Second)))
	req, err := http.NewRequest("GET", target+"/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	req.Header = http.Header{"Connection": {"Upgrade"}, "Upgrade": {"websocket"}, "Sec-Websocket-Version": {"13"}, "Sec-Websocket-Key": {"dGhlIHNhbXBsZSBub25jZQ=="}, "Sec-Websocket-Extensions": {"permessage-deflate; client_no_context_takeover; server_no_context_takeover"}}
	require.NoError(t, req.Write(c))
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, req)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	return &compressionPeer{c, br, resp}
}
func (p *compressionPeer) singleFrame(t *testing.T) (byte, []byte) {
	t.Helper()
	var h [2]byte
	_, err := io.ReadFull(p.reader, h[:])
	require.NoError(t, err)
	n := uint64(h[1] & 127)
	switch n {
	case 126:
		var b [2]byte
		_, err = io.ReadFull(p.reader, b[:])
		n = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		_, err = io.ReadFull(p.reader, b[:])
		n = binary.BigEndian.Uint64(b[:])
	}
	require.NoError(t, err)
	require.Zero(t, h[1]&128)
	require.Less(t, n, uint64(2_000_000))
	b := make([]byte, int(n))
	_, err = io.ReadFull(p.reader, b)
	require.NoError(t, err)
	return h[0], b
}
func (p *compressionPeer) frame(t *testing.T) (byte, []byte) {
	t.Helper()
	flags, data := p.singleFrame(t)
	last := flags
	for last&128 == 0 {
		var next []byte
		last, next = p.singleFrame(t)
		require.Zero(t, last&0x7f, "continuation frame must not set RSV1 or a new opcode")
		data = append(data, next...)
	}
	return flags, data
}
func (p *compressionPeer) send(t *testing.T, flags byte, data []byte) {
	t.Helper()
	h := []byte{flags, 128}
	switch {
	case len(data) < 126:
		h[1] |= byte(len(data))
	case len(data) <= 65535:
		h[1] |= 126
		h = binary.BigEndian.AppendUint16(h, uint16(len(data)))
	default:
		h[1] |= 127
		h = binary.BigEndian.AppendUint64(h, uint64(len(data)))
	}
	h = append(h, 1, 2, 3, 4)
	masked := bytes.Clone(data)
	for i := range masked {
		masked[i] ^= byte(i%4 + 1)
	}
	_, err := p.Write(append(h, masked...))
	require.NoError(t, err)
}
func compressionServer(t *testing.T, opts *Options) (*Server, *httptest.Server) {
	t.Helper()
	s := NewServer(opts)
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	return s, ts
}
func compressionAccept(t *testing.T, s *Server) Conn {
	t.Helper()
	out := make(chan Conn, 1)
	go func() { c, _ := s.Accept(); out <- c }()
	select {
	case c := <-out:
		require.NotNil(t, c)
		return c
	case <-time.After(time.Second):
		t.Fatal("session not accepted")
		return nil
	}
}

// Covers E8
func TestPerMessageDeflateNegotiated(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			s, ts := compressionServer(t, &Options{PerMessageDeflate: enabled})
			peer := compressionDial(t, ts.URL)
			require.Equal(t, enabled, strings.Contains(peer.response.Header.Get("Sec-WebSocket-Extensions"), "permessage-deflate"))
			flags, _ := peer.frame(t)
			require.Zero(t, flags&64)
			c := compressionAccept(t, s)
			for _, tc := range []struct {
				size     int
				compress bool
			}{{1022, true}, {1023, true}, {4096, false}, {4096, true}} {
				w, err := c.(CompressWriter).NextWriterCompress(session.TEXT, tc.compress)
				require.NoError(t, err)
				_, err = io.WriteString(w, strings.Repeat("a", tc.size))
				require.NoError(t, err)
				require.NoError(t, w.Close())
				flags, b := peer.frame(t)
				want := enabled && tc.compress && tc.size+1 >= 1024
				require.Equal(t, want, flags&64 != 0, "size=%d compress=%v", tc.size, tc.compress)
				if want {
					r := flate.NewReader(io.MultiReader(bytes.NewReader(b), bytes.NewReader([]byte{0, 0, 255, 255, 1, 0, 0, 255, 255})))
					b, err = io.ReadAll(r)
					require.NoError(t, err)
					require.NoError(t, r.Close())
				}
				require.Equal(t, "4"+strings.Repeat("a", tc.size), string(b))
			}
		})
	}
}

// Covers E8
// Covers E4
func TestPerMessageDeflateLimit(t *testing.T) {
	// Prepare the 100 MB message's compressed representation before measuring the
	// receiver. No 100 MB source buffer is allocated, even during preparation.
	var compressed bytes.Buffer
	deflater, err := flate.NewWriter(&compressed, flate.BestCompression)
	require.NoError(t, err)
	_, err = deflater.Write([]byte("4"))
	require.NoError(t, err)
	block := bytes.Repeat([]byte("a"), 32*1024)
	for remaining := 100_000_000; remaining > 0; {
		n := min(remaining, len(block))
		_, err = deflater.Write(block[:n])
		require.NoError(t, err)
		remaining -= n
	}
	require.NoError(t, deflater.Flush())
	wire := bytes.Clone(compressed.Bytes()[:compressed.Len()-4])
	require.NoError(t, deflater.Close())
	require.Less(t, len(wire), 1_000_000)
	s, ts := compressionServer(t, &Options{PerMessageDeflate: true})
	peer := compressionDial(t, ts.URL)
	_, _ = peer.frame(t)
	c := compressionAccept(t, s)
	reads := parityReader(c)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	peer.send(t, 0xc1, wire)
	flags, body := peer.frame(t)
	require.Equal(t, byte(8), flags&15)
	require.GreaterOrEqual(t, len(body), 2)
	require.Equal(t, uint16(1009), binary.BigEndian.Uint16(body))
	select {
	case result := <-reads:
		require.Error(t, result.err)
	case <-time.After(time.Second):
		t.Fatal("inflated limit did not stop reader")
	}
	runtime.ReadMemStats(&after)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(20_000_000), "inflated message must be rejected without allocating its full body")
	t.Logf("wire=%d bytes, receiver allocations=%d bytes", len(wire), after.TotalAlloc-before.TotalAlloc)
}

// Covers E9
func TestPollingHTTPCompression(t *testing.T) {
	for _, binaryMode := range []bool{false, true} {
		t.Run(strconv.FormatBool(binaryMode), func(t *testing.T) {
			f := compressionPollingWire(t, binaryMode)
			target := f.poll
			boundary := 1018
			if binaryMode {
				boundary = 1017
			}
			for _, tc := range []struct {
				accept   string
				size     int
				compress bool
				encoding string
			}{{"gzip", boundary - 1, true, ""}, {"gzip", boundary, true, "gzip"}, {"gzip", 4096, true, "gzip"}, {"deflate", 4096, true, "deflate"}, {"gzip, deflate", 4096, true, "gzip"}, {"gzip;q=0, deflate", 4096, true, "deflate"}, {"gzip;q=0.1, deflate;q=1", 4096, true, "deflate"}, {"*;q=0", 4096, true, ""}, {"", 4096, true, ""}, {"gzip", 3, true, ""}, {"gzip", 4096, false, ""}, {"gzip", 0, true, ""}} {
				done := make(chan error, 1)
				go func() {
					w, err := f.conn.(CompressWriter).NextWriterCompress(session.TEXT, tc.compress)
					if err == nil {
						_, err = io.WriteString(w, strings.Repeat("x", tc.size))
						if closeErr := w.Close(); err == nil {
							err = closeErr
						}
					}
					done <- err
				}()
				req, err := http.NewRequest("GET", target, nil)
				require.NoError(t, err)
				req.Header.Set("Accept-Encoding", tc.accept)
				resp, err := f.client.Do(req)
				require.NoError(t, err)
				require.Equal(t, tc.encoding, resp.Header.Get("Content-Encoding"))
				require.Contains(t, resp.Header.Values("Vary"), "Accept-Encoding")
				var reader io.Reader = resp.Body
				switch tc.encoding {
				case "gzip":
					r, err := gzip.NewReader(resp.Body)
					require.NoError(t, err)
					defer func() { _ = r.Close() }()
					reader = r
				case "deflate":
					r, err := zlib.NewReader(resp.Body)
					require.NoError(t, err)
					defer func() { _ = r.Close() }()
					reader = r
				}
				b, err := io.ReadAll(reader)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				require.NoError(t, <-done)
				require.True(t, bytes.HasSuffix(b, []byte("4"+strings.Repeat("x", tc.size))))
			}
		})
	}
	t.Run("disabled", func(t *testing.T) {
		f := newParityWire(t, &Options{DisableHTTPCompression: true})
		done := parityWrite(f.conn, strings.Repeat("x", 4096))
		req, err := http.NewRequest("GET", f.poll, nil)
		require.NoError(t, err)
		req.Header.Set("Accept-Encoding", "gzip")
		resp, err := f.client.Do(req)
		require.NoError(t, err)
		require.Empty(t, resp.Header.Get("Content-Encoding"))
		_, err = io.Copy(io.Discard, resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.NoError(t, <-done)
	})
}

// Covers E10
func TestHandshakeCookie(t *testing.T) {
	expires := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	cases := []struct {
		name string
		opts *Options
		want *http.Cookie
	}{
		{"default", nil, &http.Cookie{Name: "io", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode}},
		{"custom", &Options{Cookie: &http.Cookie{Name: "session", Path: "/chat", HttpOnly: true}}, &http.Cookie{Name: "session", Path: "/chat", HttpOnly: true}},
		{"httpOnly off", &Options{Cookie: &http.Cookie{Path: "/"}}, &http.Cookie{Name: "io", Path: "/"}},
		{"no path", &Options{Cookie: &http.Cookie{}}, &http.Cookie{Name: "io"}},
		{"full", &Options{Cookie: &http.Cookie{Name: "custom", Path: "/", Domain: "example.com", Expires: expires, MaxAge: 3600, Secure: true, SameSite: http.SameSiteLaxMode}}, &http.Cookie{Name: "custom", Path: "/", Domain: "example.com", Expires: expires, MaxAge: 3600, Secure: true, SameSite: http.SameSiteLaxMode}},
		{"off", &Options{DisableCookie: true}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, ts := compressionServer(t, tc.opts)
			cl := &http.Client{Timeout: 2 * time.Second}
			defer cl.CloseIdleConnections()
			response, err := cl.Get(ts.URL + "/?EIO=3&transport=polling&b64=1")
			require.NoError(t, err)
			data, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			_, open, ok := strings.Cut(string(data), ":0")
			require.True(t, ok)
			var params eioparser.ConnParameters
			require.NoError(t, json.Unmarshal([]byte(open), &params))
			check := func(resp *http.Response) {
				t.Helper()
				if tc.want == nil {
					require.Empty(t, resp.Cookies())
					return
				}
				want := *tc.want
				want.Value = params.SID
				require.Equal(t, want.String(), resp.Header.Get("Set-Cookie"))
			}
			check(response)
			c := compressionAccept(t, s)
			done := parityWrite(c, "later")
			later, err := cl.Get(ts.URL + "/?EIO=3&transport=polling&b64=1&sid=" + params.SID)
			require.NoError(t, err)
			check(later)
			_, err = io.Copy(io.Discard, later.Body)
			require.NoError(t, err)
			require.NoError(t, later.Body.Close())
			require.NoError(t, <-done)
			req, err := http.NewRequest("OPTIONS", ts.URL+"/?EIO=3&transport=polling&sid="+params.SID, nil)
			require.NoError(t, err)
			options, err := cl.Do(req)
			require.NoError(t, err)
			check(options)
			require.NoError(t, options.Body.Close())
			reads := parityReader(c)
			post, err := http.NewRequest(http.MethodPost, ts.URL+"/?EIO=3&transport=polling&sid="+params.SID, strings.NewReader("2:4x"))
			require.NoError(t, err)
			post.Header.Set("Content-Type", "text/plain; charset=UTF-8")
			posted, err := cl.Do(post)
			require.NoError(t, err)
			check(posted)
			body, err := io.ReadAll(posted.Body)
			require.NoError(t, err)
			require.NoError(t, posted.Body.Close())
			require.Equal(t, "ok", string(body))
			parityMessage(t, reads, "x")
			if tc.opts != nil && tc.opts.Cookie != nil {
				require.Empty(t, tc.opts.Cookie.Value, "caller cookie mutated")
			}
			peer := compressionDial(t, ts.URL)
			require.Empty(t, peer.response.Cookies())
		})
	}
}

// A binary polling handshake is decoded separately from the text-only fixture.
func compressionPollingWire(t *testing.T, binaryMode bool) *parityWire {
	t.Helper()
	if !binaryMode {
		return newParityWire(t, nil)
	}
	s, ts := compressionServer(t, nil)
	cl := &http.Client{Timeout: 2 * time.Second}
	t.Cleanup(cl.CloseIdleConnections)
	resp, err := cl.Get(ts.URL + "/?EIO=3&transport=polling")
	require.NoError(t, err)
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	separator := bytes.IndexByte(data, 255)
	require.GreaterOrEqual(t, separator, 0)
	require.Greater(t, len(data), separator+1)
	require.Equal(t, byte('0'), data[separator+1])
	var params eioparser.ConnParameters
	require.NoError(t, json.Unmarshal(data[separator+2:], &params))
	return &parityWire{srv: s, http: ts, client: cl, poll: ts.URL + "/?EIO=3&transport=polling&sid=" + params.SID, conn: compressionAccept(t, s), params: params}
}

// Covers E8
// Covers E19
func TestPerMessageDeflateLimitKeepsErrorBeforeTransportClose(t *testing.T) {
	for _, mode := range []string{"read", "discard"} {
		t.Run(mode, func(t *testing.T) {
			var compressed bytes.Buffer
			deflater, err := flate.NewWriter(&compressed, flate.BestCompression)
			require.NoError(t, err)
			_, err = io.WriteString(deflater, "4"+strings.Repeat("a", 100_000))
			require.NoError(t, err)
			require.NoError(t, deflater.Flush())
			wire := bytes.Clone(compressed.Bytes()[:compressed.Len()-4])
			require.NoError(t, deflater.Close())
			entered, release := make(chan error, 1), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			s, ts := compressionServer(t, &Options{PerMessageDeflate: true, MaxHTTPBufferSize: 1024, Hooks: &Hooks{Error: func(_ Conn, err error) { entered <- err; <-release }}})
			peer := compressionDial(t, ts.URL)
			_, _ = peer.frame(t)
			c := compressionAccept(t, s)
			readDone := make(chan error, 1)
			go func() {
				_, r, readErr := c.NextReader()
				if readErr == nil {
					if mode == "read" {
						_, readErr = io.Copy(io.Discard, r)
					}
					if closeErr := r.Close(); readErr == nil {
						readErr = closeErr
					}
				}
				_ = c.Close()
				readDone <- readErr
			}()
			peer.send(t, 0xc1, wire)
			flags, body := peer.frame(t)
			require.Equal(t, byte(8), flags&15)
			require.GreaterOrEqual(t, len(body), 2)
			require.Equal(t, uint16(1009), binary.BigEndian.Uint16(body))
			select {
			case cause := <-entered:
				require.ErrorIs(t, cause, gorilla.ErrReadLimit)
			case <-time.After(time.Second):
				t.Fatal("inflated limit did not reach Error hook")
			}
			require.NoError(t, peer.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
			_, err = peer.reader.ReadByte()
			var timeout net.Error
			require.ErrorAs(t, err, &timeout, "physical transport must remain open until Error returns")
			require.True(t, timeout.Timeout())
			unblock()
			select {
			case readErr := <-readDone:
				require.ErrorIs(t, readErr, gorilla.ErrReadLimit)
			case <-time.After(time.Second):
				t.Fatal("session did not close after Error returned")
			}
			require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
			_, err = peer.reader.ReadByte()
			require.ErrorIs(t, err, io.EOF, "session owns the physical close after Error returns")
		})
	}
}

// Covers E8
// Covers E4
// Covers E19
func TestPerMessageDeflateControlPacketLimit(t *testing.T) {
	for _, kind := range []byte{'0', '1', '2', '3', '5', '6', '9'} {
		t.Run(string(kind), func(t *testing.T) {
			var compressed bytes.Buffer
			deflater, err := flate.NewWriter(&compressed, flate.BestCompression)
			require.NoError(t, err)
			_, err = deflater.Write(append([]byte{kind}, bytes.Repeat([]byte("a"), 100_000)...))
			require.NoError(t, err)
			require.NoError(t, deflater.Flush())
			wire := bytes.Clone(compressed.Bytes()[:compressed.Len()-4])
			require.NoError(t, deflater.Close())
			causes := make(chan error, 2)
			s, ts := compressionServer(t, &Options{PerMessageDeflate: true, MaxHTTPBufferSize: 1024, Hooks: &Hooks{Error: func(_ Conn, err error) { causes <- err }}})
			peer := compressionDial(t, ts.URL)
			_, _ = peer.frame(t)
			c := compressionAccept(t, s)
			done := make(chan parityRead, 1)
			go func() {
				_, r, readErr := c.NextReader()
				var data []byte
				if readErr == nil {
					data, readErr = io.ReadAll(r)
					_ = r.Close()
				}
				done <- parityRead{message: string(data), err: readErr}
			}()
			peer.send(t, 0xc1, wire)
			// A correct close may reject this write. It must never deliver this packet.
			after := []byte(`42["after-limit"]`)
			for i := range after {
				after[i] ^= byte(i%4 + 1)
			}
			_, _ = peer.Write(append([]byte{0x81, 128 | byte(len(after)), 1, 2, 3, 4}, after...))
			flags, body := peer.frame(t)
			require.Equal(t, byte(8), flags&15)
			require.GreaterOrEqual(t, len(body), 2)
			require.Equal(t, uint16(1009), binary.BigEndian.Uint16(body))
			select {
			case got := <-done:
				require.Error(t, got.err, "application packet delivered after a rejected control packet: %q", got.message)
				require.Empty(t, got.message)
				var closed *CloseError
				require.ErrorAs(t, got.err, &closed)
				require.Equal(t, ReasonTransportError, closed.Reason)
				require.ErrorIs(t, closed.Err, gorilla.ErrReadLimit)
			case <-time.After(time.Second):
				t.Fatal("session kept reading after inflated limit")
			}
			select {
			case cause := <-causes:
				require.ErrorIs(t, cause, gorilla.ErrReadLimit)
			default:
				t.Fatal("limit did not notify Error hook")
			}
			require.Empty(t, causes, "Error hook must fire once")
			require.Equal(t, StateClosed, c.ReadyState())
		})
	}
}
