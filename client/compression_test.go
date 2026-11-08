package client_test

import (
	"bytes"
	"encoding/binary"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

// recordedCompression collects the bytes passed through one direction of a connection.
// A write is recorded after the network write returns, so the peer can receive the
// frame and the test can receive the resulting ACK before the append. writes counts the
// writes between those two points, and snapshot waits until none remains.
type recordedCompression struct {
	sync.Mutex
	data   []byte
	writes int
	idle   chan struct{} // closed when writes drops to zero
	// Hooks of TestClientCompressRecordingWaitsForWrite; nil elsewhere.
	afterWrite func(p []byte) // runs between the network write and the append
	waiting    func()         // runs when snapshot waits for a write
}

func (r *recordedCompression) append(p []byte) { r.Lock(); r.data = append(r.data, p...); r.Unlock() }

func (r *recordedCompression) beginWrite() {
	r.Lock()
	if r.writes == 0 {
		r.idle = make(chan struct{})
	}
	r.writes++
	r.Unlock()
}

func (r *recordedCompression) endWrite(p []byte) {
	r.Lock()
	r.data = append(r.data, p...)
	r.writes--
	if r.writes == 0 {
		close(r.idle)
	}
	r.Unlock()
}

// snapshot returns the recorded bytes once every write that has reached the network is
// recorded. It does not wait for reads: the reading goroutine stays blocked in Read.
func (r *recordedCompression) snapshot(t *testing.T) []byte {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		r.Lock()
		if r.writes == 0 {
			data := bytes.Clone(r.data)
			r.Unlock()
			return data
		}
		idle := r.idle
		r.Unlock()
		if r.waiting != nil {
			r.waiting()
		}
		select {
		case <-idle:
		case <-timeout:
			t.Fatal("recording of a completed write timed out")
			return nil
		}
	}
}

type recordingCompressionConn struct {
	net.Conn
	record     *recordedCompression
	recordRead bool
}

func (c recordingCompressionConn) Write(p []byte) (int, error) {
	if c.recordRead {
		return c.Conn.Write(p)
	}
	c.record.beginWrite()
	n, err := c.Conn.Write(p)
	if c.record.afterWrite != nil {
		c.record.afterWrite(p[:n])
	}
	c.record.endWrite(p[:n])
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

func TestClientCompress(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[enabled], func(t *testing.T) {
			clientCompressScenario(t, enabled, new(recordedCompression), nil)
		})
	}
}

// The client writes the last binary attachment, the server receives it and ACKs, and the
// writing goroutine is held before it records the frame. The snapshot taken after the ACK
// must wait for that recording; the write is released only once the snapshot waits.
func TestClientCompressRecordingWaitsForWrite(t *testing.T) {
	var armed atomic.Bool
	var message byte // opcode of the message being written; only the writer uses it
	paused := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWrite := func() { releaseOnce.Do(func() { close(release) }) }
	outgoing := &recordedCompression{
		afterWrite: func(p []byte) {
			if len(p) == 0 {
				return
			}
			if op := p[0] & 15; op != 0 {
				message = op
			}
			// The final frame of a binary message written after arming.
			if message == 2 && p[0]&128 != 0 && armed.CompareAndSwap(true, false) {
				close(paused)
				<-release
			}
		},
		waiting: releaseWrite,
	}
	clientCompressScenario(t, true, outgoing, func() {
		// Runs before the client's Close cleanup, which would wait for the held write.
		t.Cleanup(releaseWrite)
		armed.Store(true)
	})
	select {
	case <-paused:
	default:
		t.Fatal("the final binary frame was not held before recording")
	}
}

// clientCompressScenario emits two text/binary packets, uncompressed and then compressed,
// and checks the compression flag of each frame and the negotiated extension. beforeLast,
// if set, runs after the first ACK.
func clientCompressScenario(t *testing.T, enabled bool, outgoing *recordedCompression, beforeLast func()) {
	t.Helper()
	_, ts := newCompressedServer(t, nil)
	// Echo ACKs wait for the complete text/binary packet.
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
		if flag && beforeLast != nil {
			beforeLast()
		}
		ack := make(chan string, 1)
		c.Compress(flag).Emit("echo", value, &parser.Buffer{Data: []byte(value)}, func(v string) { ack <- v })
		require.Equal(t, value, compressionReceive(t, ack))
	}
	flags := compressionWireFlags(t, outgoing.snapshot(t))
	require.Equal(t, []bool{false, false, enabled, enabled}, flags, "flag applies to text and binary attachment")
	response := incoming.snapshot(t)
	headerEnd := bytes.Index(response, []byte("\r\n\r\n"))
	require.GreaterOrEqual(t, headerEnd, 0)
	require.Equal(t, enabled, bytes.Contains(bytes.ToLower(response[:headerEnd]), []byte("sec-websocket-extensions: permessage-deflate")))
}
