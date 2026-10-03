package framing

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func connectionPair(t *testing.T, limit int) (*Conn, net.Conn) {
	t.Helper()
	server, peer := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = peer.Close() })
	for _, c := range []net.Conn{server, peer} {
		if err := c.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	c, err := New(server, nil, false, limit)
	if err != nil {
		t.Fatal(err)
	}
	return c, peer
}

func TestMessagesBothDirections(t *testing.T) {
	server, peer := connectionPair(t, 1<<20)
	client, err := New(peer, nil, true, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]*Conn{{server, client}, {client, server}} {
		for _, tc := range []struct {
			op   ws.OpCode
			data []byte
		}{
			{ws.OpText, []byte("4€🙂\x1ehello")},
			{ws.OpBinary, []byte{0, 4, 255}},
			{ws.OpBinary, nil}, {ws.OpText, nil},
			{ws.OpBinary, bytes.Repeat([]byte{255}, 70000)},
		} {
			written := make(chan error, 1)
			go func() { written <- pair[0].WriteMessage(tc.op, tc.data) }()
			op, data, err := pair[1].ReadMessage()
			if err != nil || op != tc.op || !bytes.Equal(data, tc.data) {
				t.Fatalf("ReadMessage = %v %d bytes %v; want %v %d bytes", op, len(data), err, tc.op, len(tc.data))
			}
			if err := <-written; err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestFragmentationAndIntermediatePing(t *testing.T) {
	server, peer := connectionPair(t, 3)
	done := make(chan error, 1)
	go func() {
		first := ws.NewTextFrame([]byte{0xe2})
		first.Header.Fin = false
		for _, f := range []ws.Frame{first, ws.NewPingFrame([]byte("probe"))} {
			if err := ws.WriteFrame(peer, ws.MaskFrameInPlace(f)); err != nil {
				done <- err
				return
			}
		}
		pong, err := ws.ReadFrame(peer)
		if err != nil || pong.Header.OpCode != ws.OpPong || pong.Header.Masked || string(pong.Payload) != "probe" {
			done <- fmt.Errorf("invalid pong %+v: %v", pong, err)
			return
		}
		last := ws.NewFrame(ws.OpContinuation, true, []byte{0x82, 0xac})
		done <- ws.WriteFrame(peer, ws.MaskFrameInPlace(last))
	}()
	op, body, err := server.ReadMessage()
	if err != nil || op != ws.OpText || string(body) != "€" {
		t.Fatalf("fragmented message = %v %q %v", op, body, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneControlFrames(t *testing.T) {
	server, peer := connectionPair(t, 1)
	done := make(chan error, 1)
	go func() {
		for _, f := range []ws.Frame{ws.NewPongFrame([]byte("ignored")), ws.NewPingFrame(nil)} {
			// WriteFrame also writes an empty payload. net.Pipe blocks that write
			// until another read, unlike TCP; send empty controls as headers only.
			f = ws.MaskFrameInPlace(f)
			var err error
			if len(f.Payload) == 0 {
				err = ws.WriteHeader(peer, f.Header)
			} else {
				err = ws.WriteFrame(peer, f)
			}
			if err != nil {
				done <- err
				return
			}
		}
		pong, err := ws.ReadFrame(peer)
		if err != nil || pong.Header.OpCode != ws.OpPong || len(pong.Payload) != 0 {
			done <- fmt.Errorf("invalid empty pong %+v: %v", pong, err)
			return
		}
		done <- ws.WriteFrame(peer, ws.MaskFrameInPlace(ws.NewTextFrame([]byte("4"))))
	}()
	op, body, err := server.ReadMessage()
	if err != nil || op != ws.OpText || string(body) != "4" {
		t.Fatalf("message after controls = %v %q %v", op, body, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestReadFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		frames []ws.Frame
		mask   bool
		want   error
	}{
		{"large-frame", []ws.Frame{ws.NewBinaryFrame([]byte("123456"))}, true, ErrTooLarge},
		{"large-fragments", []ws.Frame{ws.NewFrame(ws.OpBinary, false, []byte("123")), ws.NewFrame(ws.OpContinuation, true, []byte("456"))}, true, ErrTooLarge},
		{"unmasked-client", []ws.Frame{ws.NewTextFrame([]byte("4"))}, false, nil},
		{"invalid-utf8", []ws.Frame{ws.NewTextFrame([]byte{0xff})}, true, wsutil.ErrInvalidUTF8},
		{"unexpected-continuation", []ws.Frame{ws.NewFrame(ws.OpContinuation, true, []byte("x"))}, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, peer := connectionPair(t, 5)
			done := make(chan struct{})
			go func() {
				defer close(done)
				for _, f := range tc.frames {
					if tc.mask {
						f = ws.MaskFrameInPlace(f)
					}
					if err := ws.WriteFrame(peer, f); err != nil {
						return // Rejection may close the connection before the body is sent.
					}
				}
			}()
			_, body, err := server.ReadMessage()
			if body != nil || err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("ReadMessage = %q %v; want %v", body, err, tc.want)
			}
			<-done
		})
	}
}

func TestTruncatedAndClosed(t *testing.T) {
	server, peer := connectionPair(t, 10)
	go func() {
		_ = ws.WriteHeader(peer, ws.Header{Fin: true, OpCode: ws.OpBinary, Masked: true, Length: 2})
		_, _ = peer.Write([]byte{1})
		_ = peer.Close()
	}()
	if _, body, err := server.ReadMessage(); body != nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated message = %q %v", body, err)
	}
	server, peer = connectionPair(t, 10)
	done := make(chan error, 1)
	go func() {
		f := ws.NewCloseFrame(ws.NewCloseFrameBody(ws.StatusNormalClosure, "bye"))
		if err := ws.WriteFrame(peer, ws.MaskFrameInPlace(f)); err != nil {
			done <- err
			return
		}
		reply, err := ws.ReadFrame(peer)
		if err == nil && reply.Header.OpCode != ws.OpClose {
			err = errors.New("missing close reply")
		}
		done <- err
	}()
	_, body, err := server.ReadMessage()
	var closed wsutil.ClosedError
	if body != nil || !errors.As(err, &closed) || closed.Code != ws.StatusNormalClosure {
		t.Fatalf("close frame = %q %v", body, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentWriters(t *testing.T) {
	server, peer := connectionPair(t, 100)
	client, err := New(peer, nil, true, 100)
	if err != nil {
		t.Fatal(err)
	}
	const count = 32
	var wg sync.WaitGroup
	errCh := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errCh <- server.WriteMessage(ws.OpText, []byte(fmt.Sprintf("message-%d", i)))
		}(i)
	}
	seen := make(map[string]bool)
	for i := 0; i < count; i++ {
		op, body, err := client.ReadMessage()
		if err != nil || op != ws.OpText || seen[string(body)] {
			t.Fatalf("concurrent message = %v %q %v", op, body, err)
		}
		seen[string(body)] = true
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

type delayedCloseConn struct {
	net.Conn
	closing chan struct{}
	release chan struct{}
}

func (c *delayedCloseConn) Close() error {
	close(c.closing)
	<-c.release
	return c.Conn.Close()
}

func TestCloseReplyPreventsConcurrentDataWrite(t *testing.T) {
	server, peer := connectionPair(t, 100)
	delayed := &delayedCloseConn{Conn: server.raw, closing: make(chan struct{}), release: make(chan struct{})}
	server.raw = delayed
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(delayed.release) }) }
	defer release()
	peerDone := make(chan error, 1)
	go func() {
		f := ws.NewCloseFrame(ws.NewCloseFrameBody(ws.StatusNormalClosure, "bye"))
		if err := ws.WriteFrame(peer, ws.MaskFrameInPlace(f)); err != nil {
			peerDone <- err
			return
		}
		reply, err := ws.ReadFrame(peer)
		if err != nil || reply.Header.OpCode != ws.OpClose {
			peerDone <- fmt.Errorf("missing close reply: %+v %v", reply, err)
			return
		}
		if next, err := ws.ReadFrame(peer); err == nil {
			peerDone <- fmt.Errorf("frame after close reply: %+v", next)
			return
		}
		peerDone <- nil
	}()
	readDone := make(chan error, 1)
	go func() { _, _, err := server.ReadMessage(); readDone <- err }()
	select {
	case <-delayed.closing:
	case <-time.After(2 * time.Second):
		t.Fatal("close reply did not reach connection shutdown")
	}
	written := make(chan error, 1)
	go func() { written <- server.WriteMessage(ws.OpText, []byte("too late")) }()
	// Keep TCP open after the close reply so that a writer escaping the mutex
	// would succeed. A correct writer waits for shutdown, then returns an error.
	select {
	case err := <-written:
		t.Errorf("writer returned while close was still pending: %v", err)
	case <-time.After(100 * time.Millisecond):
		release()
		if err := <-written; err == nil {
			t.Error("data write after close succeeded")
		}
	}
	release()
	var closed wsutil.ClosedError
	if err := <-readDone; !errors.As(err, &closed) {
		t.Errorf("ReadMessage returned %v; want ClosedError", err)
	}
	if err := <-peerDone; err != nil {
		t.Error(err)
	}
}

func TestLocalErrorsAndCloseUnblocks(t *testing.T) {
	if _, err := New(nil, nil, false, 0); !errors.Is(err, ErrInvalidLimit) {
		t.Fatal(err)
	}
	server, peer := connectionPair(t, 5)
	for _, tc := range []struct {
		op   ws.OpCode
		body []byte
		want error
	}{{ws.OpPing, nil, ErrMessageType}, {ws.OpBinary, []byte("123456"), ErrTooLarge}, {ws.OpText, []byte{0xff}, wsutil.ErrInvalidUTF8}} {
		if err := server.WriteMessage(tc.op, tc.body); !errors.Is(err, tc.want) {
			t.Fatalf("WriteMessage = %v; want %v", err, tc.want)
		}
	}
	done := make(chan error, 1)
	go func() { _, _, err := server.ReadMessage(); done <- err }()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("Close did not terminate ReadMessage")
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	_ = peer.Close()
	if err := server.WriteMessage(ws.OpText, []byte("4")); err == nil {
		t.Fatal("write on a closed connection succeeded")
	}
}
