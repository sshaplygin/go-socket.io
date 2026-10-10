package websocket

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/gobwas/ws"
)

// sinkConn accepts every write, as the peer of a connection that is being fuzzed.
type sinkConn struct{ net.Conn }

func (sinkConn) Write(p []byte) (int, error)      { return len(p), nil }
func (sinkConn) Close() error                     { return nil }
func (sinkConn) SetWriteDeadline(time.Time) error { return nil }
func (sinkConn) SetReadDeadline(time.Time) error  { return nil }
func (sinkConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (sinkConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (sinkConn) SetDeadline(time.Time) error      { return nil }
func (sinkConn) Read([]byte) (int, error)         { return 0, net.ErrClosed }

// frames encodes fs as a peer would send them.
func frames(masked bool, fs ...ws.Frame) []byte {
	var b bytes.Buffer
	for _, f := range fs {
		if masked {
			f = ws.MaskFrameInPlace(f)
		}
		_ = ws.WriteFrame(&b, f)
	}
	return b.Bytes()
}

// FuzzReadMessage feeds arbitrary bytes, as frames from a peer, to both endpoint
// roles: reading must return messages within the limit or an error, never panic
// or hang, and a failed connection must stay failed.
func FuzzReadMessage(f *testing.F) {
	text, bin := ws.NewTextFrame([]byte("4hello")), ws.NewBinaryFrame([]byte{0, 1, 2})
	first := ws.NewFrame(ws.OpText, false, []byte{'4', 0xe2})
	last := ws.NewFrame(ws.OpContinuation, true, []byte{0x82, 0xac})
	for _, seed := range [][]byte{
		frames(true, text), frames(true, bin), frames(false, text), frames(true, first, ws.NewPingFrame([]byte("p")), last),
		frames(true, first, last), frames(true, ws.NewFrame(ws.OpContinuation, true, []byte("x"))),
		frames(true, ws.NewCloseFrame(ws.NewCloseFrameBody(ws.StatusNormalClosure, "bye"))),
		frames(true, ws.NewCloseFrame([]byte{0x03, 0xed})), frames(true, ws.NewPongFrame(nil), ws.NewPingFrame(nil), text),
		frames(true, ws.NewBinaryFrame(bytes.Repeat([]byte{7}, 200))),
		{0x81}, {0x81, 0xff}, {0x82, 0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, {0xff, 0x00},
	} {
		f.Add(seed, true)
		f.Add(seed, false)
	}
	f.Fuzz(func(t *testing.T, data []byte, clientSide bool) {
		const limit = 64
		c, err := newMessageConn(sinkConn{}, bytes.NewReader(data), clientSide, limit, 0)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i <= len(data); i++ { // every successful read consumes at least a header
			op, body, err := c.readMessage()
			if err != nil {
				if body != nil {
					t.Fatalf("partial message %q with error %v", body, err)
				}
				if _, _, again := c.readMessage(); again == nil {
					t.Fatal("read succeeded on a failed connection")
				}
				return
			}
			if op != ws.OpText && op != ws.OpBinary {
				t.Fatalf("opcode %v", op)
			}
			if len(body) > limit {
				t.Fatalf("message of %d bytes over the limit", len(body))
			}
		}
	})
}
