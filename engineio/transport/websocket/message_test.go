package websocket

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func connectionPair(t *testing.T, limit int) (*messageConn, net.Conn) {
	t.Helper()
	server, peer := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = peer.Close() })
	for _, c := range []net.Conn{server, peer} {
		if err := c.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	c, err := newMessageConn(server, nil, false, limit, 0)
	if err != nil {
		t.Fatal(err)
	}
	return c, peer
}

func TestMessageBothDirections(t *testing.T) {
	server, peer := connectionPair(t, 1<<20)
	client, err := newMessageConn(peer, nil, true, 1<<20, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]*messageConn{{server, client}, {client, server}} {
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
			go func() { written <- pair[0].writeMessage(tc.op, tc.data) }()
			op, data, err := pair[1].readMessage()
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
	op, body, err := server.readMessage()
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
	op, body, err := server.readMessage()
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
		status ws.StatusCode
	}{
		{"large-frame", []ws.Frame{ws.NewBinaryFrame([]byte("123456"))}, true, ErrTooLarge, ws.StatusMessageTooBig},
		{"large-fragments", []ws.Frame{ws.NewFrame(ws.OpBinary, false, []byte("123")), ws.NewFrame(ws.OpContinuation, true, []byte("456"))}, true, ErrTooLarge, ws.StatusMessageTooBig},
		{"unmasked-client", []ws.Frame{ws.NewTextFrame([]byte("4"))}, false, ws.ErrProtocolMaskRequired, ws.StatusProtocolError},
		{"invalid-utf8", []ws.Frame{ws.NewTextFrame([]byte{0xff})}, true, wsutil.ErrInvalidUTF8, ws.StatusInvalidFramePayloadData},
		{"unexpected-continuation", []ws.Frame{ws.NewFrame(ws.OpContinuation, true, []byte("x"))}, true, ws.ErrProtocolContinuationUnexpected, ws.StatusProtocolError},
		{"reserved-bits", []ws.Frame{{Header: ws.Header{Fin: true, Rsv: ws.Rsv(true, false, false), OpCode: ws.OpText, Masked: true, Length: 1}, Payload: []byte("4")}}, true, ws.ErrProtocolNonZeroRsv, ws.StatusProtocolError},
		{"reserved-opcode", []ws.Frame{ws.NewFrame(ws.OpCode(3), true, []byte("x"))}, true, ws.ErrProtocolOpCodeReserved, ws.StatusProtocolError},
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
			// The peer must receive a close frame with the status of the violation
			// before the connection closes, not a bare TCP close.
			reply := make(chan ws.Frame, 1)
			go func() {
				f, err := ws.ReadFrame(peer)
				if err != nil {
					t.Errorf("no close frame: %v", err)
				}
				reply <- f
			}()
			_, body, err := server.readMessage()
			if body != nil || err == nil || !errors.Is(err, tc.want) {
				t.Fatalf("readMessage = %q %v; want %v", body, err, tc.want)
			}
			f := <-reply
			code, _ := ws.ParseCloseFrameData(f.Payload)
			if f.Header.OpCode != ws.OpClose || f.Header.Masked || code != tc.status {
				t.Fatalf("reply = %v masked=%v status=%d; want close with status %d", f.Header.OpCode, f.Header.Masked, code, tc.status)
			}
			<-done
		})
	}
}

// A client reports a violation of the server with a masked close frame.
func TestClientSideViolationClosesMasked(t *testing.T) {
	peer, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = peer.Close() })
	client, err := newMessageConn(peer, nil, true, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	reply := make(chan ws.Frame, 1)
	go func() {
		_ = ws.WriteFrame(server, ws.NewBinaryFrame([]byte("123456"))) // server frames are unmasked
	}()
	go func() {
		f, err := ws.ReadFrame(server)
		if err != nil {
			t.Errorf("no close frame: %v", err)
		}
		reply <- f
	}()
	if _, _, err := client.readMessage(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("readMessage = %v; want ErrTooLarge", err)
	}
	f := <-reply
	masked := f.Header.Masked
	if masked {
		f = ws.UnmaskFrameInPlace(f)
	}
	code, _ := ws.ParseCloseFrameData(f.Payload)
	if f.Header.OpCode != ws.OpClose || !masked || code != ws.StatusMessageTooBig {
		t.Fatalf("reply = %v masked=%v status=%d", f.Header.OpCode, masked, code)
	}
}

// An I/O failure and a close started by the peer send no close status of their own.
func TestNoStatusWithoutViolation(t *testing.T) {
	for _, err := range []error{io.EOF, io.ErrUnexpectedEOF, os.ErrDeadlineExceeded, wsutil.ClosedError{Code: ws.StatusNormalClosure}} {
		if code := closeStatus(err); code != 0 {
			t.Errorf("closeStatus(%v) = %d; want none", err, code)
		}
	}
}

func TestTruncatedAndClosed(t *testing.T) {
	server, peer := connectionPair(t, 10)
	go func() {
		_ = ws.WriteHeader(peer, ws.Header{Fin: true, OpCode: ws.OpBinary, Masked: true, Length: 2})
		_, _ = peer.Write([]byte{1})
		_ = peer.Close()
	}()
	if _, body, err := server.readMessage(); body != nil || !errors.Is(err, io.ErrUnexpectedEOF) {
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
	_, body, err := server.readMessage()
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
	client, err := newMessageConn(peer, nil, true, 100, 0)
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
			errCh <- server.writeMessage(ws.OpText, []byte(fmt.Sprintf("message-%d", i)))
		}(i)
	}
	seen := make(map[string]bool)
	for i := 0; i < count; i++ {
		op, body, err := client.readMessage()
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
	go func() { _, _, err := server.readMessage(); readDone <- err }()
	select {
	case <-delayed.closing:
	case <-time.After(2 * time.Second):
		t.Fatal("close reply did not reach connection shutdown")
	}
	written := make(chan error, 1)
	go func() { written <- server.writeMessage(ws.OpText, []byte("too late")) }()
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
	if _, err := newMessageConn(nil, nil, false, 0, 0); !errors.Is(err, ErrInvalidLimit) {
		t.Fatal(err)
	}
	server, peer := connectionPair(t, 5)
	for _, tc := range []struct {
		op   ws.OpCode
		body []byte
		want error
	}{{ws.OpPing, nil, ErrMessageType}, {ws.OpBinary, []byte("123456"), ErrTooLarge}, {ws.OpText, []byte{0xff}, wsutil.ErrInvalidUTF8}} {
		if err := server.writeMessage(tc.op, tc.body); !errors.Is(err, tc.want) {
			t.Fatalf("WriteMessage = %v; want %v", err, tc.want)
		}
	}
	done := make(chan error, 1)
	go func() { _, _, err := server.readMessage(); done <- err }()
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
	if err := server.writeMessage(ws.OpText, []byte("4")); err == nil {
		t.Fatal("write on a closed connection succeeded")
	}
}

// closeRecorder reports the moment the socket is really closed.
type closeRecorder struct {
	*net.TCPConn
	closed chan struct{}
	once   sync.Once
}

func (c *closeRecorder) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.TCPConn.Close()
}

// tcpPair returns the two ends of a loopback TCP connection.
func tcpPair(t *testing.T) (server, client *net.TCPConn) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	type result struct {
		c   net.Conn
		err error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := l.Accept()
		ch <- result{c, err}
	}()
	d, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	r := <-ch
	if r.err != nil {
		t.Fatal(r.err)
	}
	t.Cleanup(func() { _ = r.c.Close(); _ = d.Close() })
	return r.c.(*net.TCPConn), d.(*net.TCPConn)
}

// The consumer calls Close as soon as the read error comes back. The close frame
// must still be readable, followed by EOF, while the peer has 256 KiB or more
// unread in the socket, and the socket must be closed within closeTimeout.
func TestCloseDuringDrainKeepsFrameDeliverable(t *testing.T) {
	big := bytes.Repeat([]byte{1}, 256<<10)
	cases := []struct {
		name  string
		code  ws.StatusCode
		write func(w io.Writer) error
	}{
		{"too large", ws.StatusMessageTooBig, func(w io.Writer) error {
			return wsutil.WriteClientBinary(w, big)
		}},
		{"invalid utf-8", ws.StatusInvalidFramePayloadData, func(w io.Writer) error {
			if err := wsutil.WriteClientText(w, []byte{0xff}); err != nil {
				return err
			}
			return wsutil.WriteClientBinary(w, big)
		}},
		{"unmasked frame", ws.StatusProtocolError, func(w io.Writer) error {
			if err := ws.WriteFrame(w, ws.NewTextFrame([]byte("x"))); err != nil {
				return err
			}
			return wsutil.WriteClientBinary(w, big)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, p := tcpPair(t)
			rec := &closeRecorder{TCPConn: s, closed: make(chan struct{})}
			c, err := newMessageConn(rec, nil, false, 1024, 0)
			if err != nil {
				t.Fatal(err)
			}
			go func() { _ = tc.write(p) }()
			if _, _, err := c.readMessage(); err == nil {
				t.Fatal("readMessage succeeded; want a violation")
			}
			start := time.Now()
			_ = c.Close()
			select {
			case <-rec.closed:
				t.Fatal("Close closed the socket while the close frame was draining")
			default:
			}
			if _, _, err := c.readMessage(); !errors.Is(err, net.ErrClosed) {
				t.Errorf("readMessage after Close = %v; want net.ErrClosed", err)
			}
			if err := c.writeMessage(ws.OpBinary, []byte("x")); !errors.Is(err, net.ErrClosed) {
				t.Errorf("writeMessage after Close = %v; want net.ErrClosed", err)
			}

			if err := p.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			f, err := ws.ReadFrame(p)
			if err != nil {
				t.Fatalf("close frame: %v", err)
			}
			if f.Header.OpCode != ws.OpClose {
				t.Fatalf("opcode %v; want close", f.Header.OpCode)
			}
			if code, _ := ws.ParseCloseFrameData(f.Payload); code != tc.code {
				t.Errorf("status %d; want %d", code, tc.code)
			}
			if _, err := ws.ReadFrame(p); !errors.Is(err, io.EOF) {
				t.Errorf("after the close frame: %v; want EOF", err)
			}
			// The peer keeps its side open, so only the drain timeout ends this.
			select {
			case <-rec.closed:
			case <-time.After(closeTimeout + 3*time.Second):
				t.Fatal("socket still open after closeTimeout")
			}
			if d := time.Since(start); d > closeTimeout+2*time.Second {
				t.Errorf("socket closed after %v; want about closeTimeout", d)
			}
		})
	}
}

// Without a close frame to protect, Close closes the socket at once.
func TestCloseWithoutFrameClosesAtOnce(t *testing.T) {
	s, _ := tcpPair(t)
	rec := &closeRecorder{TCPConn: s, closed: make(chan struct{})}
	c, err := newMessageConn(rec, nil, false, 1024, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	select {
	case <-rec.closed:
	default:
		t.Fatal("Close left the socket open")
	}
}
