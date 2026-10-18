package client_test

import (
	"bytes"
	"encoding/binary"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/sshaplygin/go-socket.io/parser"
)

func newCompressedServer(t *testing.T, connected func(socketio.Conn)) (*socketio.Server, *httptest.Server) {
	t.Helper()
	s := socketio.NewServer(socketio.WithEngineOptions(engineio.WithPerMessageDeflate(true), engineio.WithCompressionThreshold(32)))
	s.OnConnect("/", func(c socketio.Conn) error {
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

type recordedCompression struct {
	sync.Mutex
	data []byte
}

func (r *recordedCompression) append(p []byte) { r.Lock(); r.data = append(r.data, p...); r.Unlock() }

func (r *recordedCompression) bytes() []byte { r.Lock(); defer r.Unlock(); return bytes.Clone(r.data) }

type recordingCompressionConn struct {
	net.Conn
	record     *recordedCompression
	recordRead bool
}

func (c recordingCompressionConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if !c.recordRead {
		c.record.append(p[:n])
	}
	return n, err
}

func (c recordingCompressionConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if c.recordRead {
		c.record.append(p[:n])
	}
	return n, err
}

func compressionWireFlags(t *testing.T, wire []byte) []bool {
	t.Helper()
	i := bytes.Index(wire, []byte("\r\n\r\n"))
	require.GreaterOrEqual(t, i, 0)
	wire = wire[i+4:]
	var flags []bool
	for len(wire) >= 2 {
		f, b := wire[0], wire[1]
		wire = wire[2:]
		n := uint64(b & 127)
		switch n {
		case 126:
			require.GreaterOrEqual(t, len(wire), 2)
			n = uint64(binary.BigEndian.Uint16(wire))
			wire = wire[2:]
		case 127:
			require.GreaterOrEqual(t, len(wire), 8)
			n = binary.BigEndian.Uint64(wire)
			wire = wire[8:]
		}
		if b&128 != 0 {
			require.GreaterOrEqual(t, len(wire), 4)
			wire = wire[4:]
		}
		require.LessOrEqual(t, n, uint64(len(wire)))
		wire = wire[n:]
		if f&15 != 0 {
			flags = append(flags, f&64 != 0)
		}
	}
	require.Empty(t, wire)
	return flags
}

// Covers C11
func TestClientCompress(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[enabled], func(t *testing.T) {
			_, ts := newCompressedServer(t, nil)
			// Echo ACKs wait for the complete text/binary packet.
			outgoing := new(recordedCompression)
			incoming := new(recordedCompression)
			transportConfig := &websocket.Transport{NetDial: func(network, addr string) (net.Conn, error) {
				c, err := net.Dial(network, addr)
				if err != nil {
					return nil, err
				}
				return recordingCompressionConn{Conn: recordingCompressionConn{Conn: c, record: outgoing}, record: incoming, recordRead: true}, nil
			}}
			cl, err := client.New(ts.URL, client.WithTransports([]transport.Transport{transportConfig}...), client.WithPerMessageDeflate(enabled), client.WithReconnection(!(true)))
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			// Install the handler before Connect; it receives binary attachments in order.
			srv := ts.Config.Handler.(*socketio.Server)
			srv.OnEvent("/", "echo", func(_ socketio.Conn, value string, b *parser.Buffer) string {
				require.Equal(t, []byte(value), b.Data)
				return value
			})
			connected := make(chan client.Conn, 1)
			cl.OnConnect(func(c client.Conn) error { connected <- c; return nil })
			require.NoError(t, cl.Connect())
			c := compressionReceive(t, connected)
			require.Equal(t, false, transportConfig.EnableCompression, "configuration must not be mutated")
			value := strings.Repeat("client", 1024)
			for _, flag := range []bool{false, true} {
				ack := make(chan string, 1)
				require.NoError(t, c.Compress(flag).Emit("echo", value, &parser.Buffer{Data: []byte(value)}, func(v string) { ack <- v }))
				require.Equal(t, value, compressionReceive(t, ack))
			}
			flags := compressionWireFlags(t, outgoing.bytes())
			require.Equal(t, []bool{false, false, enabled, enabled}, flags, "flag applies to text and binary attachment")
			response := incoming.bytes()
			headerEnd := bytes.Index(response, []byte("\r\n\r\n"))
			require.GreaterOrEqual(t, headerEnd, 0)
			require.Equal(t, enabled, bytes.Contains(bytes.ToLower(response[:headerEnd]), []byte("sec-websocket-extensions: permessage-deflate")))
		})
	}
}
