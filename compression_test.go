package socketio

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/parser"
)

type compressedPeer struct {
	net.Conn
	reader   *bufio.Reader
	response *http.Response
}

func compressedDial(t *testing.T, target string) *compressedPeer {
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
	return &compressedPeer{c, br, resp}
}
func (p *compressedPeer) singleFrame(t *testing.T) (byte, []byte) {
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
func (p *compressedPeer) frame(t *testing.T) (byte, []byte) {
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
func newCompressedServer(t *testing.T, connected func(Conn)) (*Server, *httptest.Server) {
	t.Helper()
	s := NewServer(WithEngineOptions(engineio.WithPerMessageDeflate(true), engineio.WithCompressionThreshold(32)))
	s.OnConnect("/", func(c Conn) error {
		c.Join("room")
		if connected != nil {
			connected(c)
		}
		return nil
	})
	served := make(chan struct{})
	go func() { _ = s.Serve(); close(served) }()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close(); <-served })
	return s, ts
}
func compressedConnect(t *testing.T, ts *httptest.Server) *compressedPeer {
	t.Helper()
	p := compressedDial(t, ts.URL)
	require.Contains(t, p.response.Header.Get("Sec-WebSocket-Extensions"), "permessage-deflate")
	_, _ = p.frame(t)
	_, data := p.message(t)
	require.Equal(t, "40", string(data))
	return p
}
func (p *compressedPeer) message(t *testing.T) (bool, []byte) {
	t.Helper()
	flags, data := p.frame(t)
	if flags&64 != 0 {
		r := flate.NewReader(io.MultiReader(bytes.NewReader(data), bytes.NewReader([]byte{0, 0, 255, 255, 1, 0, 0, 255, 255})))
		var err error
		data, err = io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
	}
	return flags&64 != 0, data
}
func compressionReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("compression scenario timed out")
		var zero T
		return zero
	}
}

func TestEmitCompressFlag(t *testing.T) {
	t.Run("pending and binary", func(t *testing.T) {
		value := strings.Repeat("x", 4096)
		_, ts := newCompressedServer(t, func(c Conn) {
			base := c.Compress(false)
			_ = base.Emit("plain", value, &parser.Buffer{Data: []byte(value)})
			_ = base.Compress(true).Emit("compressed", value, &parser.Buffer{Data: []byte(value)})
			_ = base.Emit("still plain", value)
			c.Emit("default", value)
		})
		p := compressedConnect(t, ts)
		for _, tc := range []struct {
			compressed bool
			contains   string
		}{{false, "plain"}, {false, value}, {true, "compressed"}, {true, value}, {false, "still plain"}, {true, "default"}} {
			compressed, data := p.message(t)
			require.Equal(t, tc.compressed, compressed)
			require.Contains(t, string(data), tc.contains)
		}
	})
	t.Run("room excludes sender", func(t *testing.T) {
		connections := make(chan Conn, 3)
		_, ts := newCompressedServer(t, func(c Conn) { connections <- c })
		sender := compressedConnect(t, ts)
		c := compressionReceive(t, connections)
		other := compressedConnect(t, ts)
		d := compressionReceive(t, connections)
		value := strings.Repeat("x", 4096)
		require.NoError(t, c.Compress(false).To("room").Emit("room", value))
		c.Emit("sender barrier", value)
		d.Emit("receiver barrier", value)
		compressed, data := sender.message(t)
		require.True(t, compressed)
		require.Contains(t, string(data), "sender barrier")
		compressed, data = other.message(t)
		require.False(t, compressed)
		require.Contains(t, string(data), "room")
		compressed, data = other.message(t)
		require.True(t, compressed)
		require.Contains(t, string(data), "receiver barrier")
	})
}

func TestBroadcastCompressFlag(t *testing.T) {
	connected := make(chan Conn, 2)
	s, ts := newCompressedServer(t, func(c Conn) { connected <- c })
	p := compressedConnect(t, ts)
	_ = compressionReceive(t, connected)
	q := compressedConnect(t, ts)
	_ = compressionReceive(t, connected)
	value := strings.Repeat("broadcast", 512)
	base := s.Of("/").Compress(false).To("room").Local()
	require.NoError(t, base.Emit("plain", value))
	require.NoError(t, base.Compress(true).Emit("compressed", value))
	require.NoError(t, base.Send(value))
	for _, peer := range []*compressedPeer{p, q} {
		for _, tc := range []struct {
			flag  bool
			event string
		}{{false, "plain"}, {true, "compressed"}, {false, "message"}} {
			flag, data := peer.message(t)
			require.Equal(t, tc.flag, flag)
			require.Contains(t, string(data), tc.event)
		}
	}
}

func TestRedisBroadcastCompress(t *testing.T) {
	redis := miniredis.RunT(t)
	local, remote := newTestRedisBroadcast(t, redis), newTestRedisBroadcast(t, redis)
	connected := make(chan Conn, 2)
	_, ts := newCompressedServer(t, func(c Conn) { connected <- c })
	a := compressedConnect(t, ts)
	c := compressionReceive(t, connected)
	b := compressedConnect(t, ts)
	d := compressionReceive(t, connected)
	local.Join("room", c)
	remote.Join("room", d)
	value := strings.Repeat("redis", 1024)
	require.NoError(t, local.SendTo(adapter.Options{Rooms: []string{"room"}, NoCompress: true}, "news", value))
	flag, data := a.message(t)
	require.False(t, flag)
	require.Contains(t, string(data), "news")
	flag, data = b.message(t)
	require.False(t, flag, "Redis preserves the remote compression override")
	require.Contains(t, string(data), "news")
	require.NoError(t, local.SendTo(adapter.Options{Rooms: []string{"room"}, Local: true, NoCompress: true}, "only local", value))
	flag, data = a.message(t)
	require.False(t, flag)
	require.Contains(t, string(data), "only local")
	d.Emit("barrier", value)
	_, data = b.message(t)
	require.Contains(t, string(data), "barrier")
}
