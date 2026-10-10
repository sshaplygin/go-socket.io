package session

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// scriptedPacket is one packet a scriptConn returns from NextReader.
type scriptedPacket struct {
	ft   frame.Type
	pt   packet.Type
	data string
	err  error
}

// writtenPacket is one packet written through a scriptConn.
type writtenPacket struct {
	ft   frame.Type
	pt   packet.Type
	data string
}

// scriptConn is a transport.Conn that replays a fixed list of inbound
// packets and records outbound ones.
type scriptConn struct {
	mu        sync.Mutex
	reads     []scriptedPacket
	writes    []writtenPacket
	writerErr error // returned by NextWriter
	wCloseErr error // returned by the writer's Close
	closed    bool
	paused    int
	resumed   int
}

func (c *scriptConn) NextReader() (frame.Type, packet.Type, io.ReadCloser, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.reads) == 0 {
		return 0, 0, nil, io.EOF
	}
	p := c.reads[0]
	c.reads = c.reads[1:]
	if p.err != nil {
		return 0, 0, nil, p.err
	}
	return p.ft, p.pt, io.NopCloser(strings.NewReader(p.data)), nil
}

func (c *scriptConn) NextWriter(ft frame.Type, pt packet.Type) (io.WriteCloser, error) {
	if c.writerErr != nil {
		return nil, c.writerErr
	}
	return &scriptWriter{c: c, ft: ft, pt: pt}, nil
}

func (c *scriptConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *scriptConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *scriptConn) written() []writtenPacket {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]writtenPacket(nil), c.writes...)
}

func (c *scriptConn) URL() url.URL                     { return url.URL{} }
func (c *scriptConn) LocalAddr() net.Addr              { return nil }
func (c *scriptConn) RemoteAddr() net.Addr             { return nil }
func (c *scriptConn) RemoteHeader() http.Header        { return nil }
func (c *scriptConn) SetReadDeadline(time.Time) error  { return nil }
func (c *scriptConn) SetWriteDeadline(time.Time) error { return nil }

type scriptWriter struct {
	c   *scriptConn
	ft  frame.Type
	pt  packet.Type
	buf bytes.Buffer
}

func (w *scriptWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *scriptWriter) Close() error {
	w.c.mu.Lock()
	w.c.writes = append(w.c.writes, writtenPacket{ft: w.ft, pt: w.pt, data: w.buf.String()})
	w.c.mu.Unlock()
	return w.c.wCloseErr
}

// pausableConn is a scriptConn that also implements Pauser, as the polling
// transport does.
type pausableConn struct{ scriptConn }

func (c *pausableConn) Pause() {
	c.mu.Lock()
	c.paused++
	c.mu.Unlock()
}

func (c *pausableConn) Resume() {
	c.mu.Lock()
	c.resumed++
	c.mu.Unlock()
}

func newTestSession(t *testing.T, conn transport.Conn, tr string) *Session {
	t.Helper()
	s, err := New(conn, "sid1", tr, transport.ConnParameters{
		PingInterval: time.Second,
		PingTimeout:  time.Second,
		Upgrades:     []string{"websocket"},
	}, nil)
	require.NoError(t, err)
	return s
}

func TestSessionAccessors(t *testing.T) {
	s := newTestSession(t, &scriptConn{}, "polling")
	require.Equal(t, "sid1", s.ID())
	require.Equal(t, "polling", s.Transport())
	s.SetContext("ctx")
	require.Equal(t, "ctx", s.Context())
	require.Equal(t, url.URL{}, s.URL())
	require.Nil(t, s.LocalAddr())
	require.Nil(t, s.RemoteAddr())
	require.Nil(t, s.RemoteHeader())
}

func TestSessionNextReaderAnswersPingAndSkipsUnknown(t *testing.T) {
	conn := &scriptConn{reads: []scriptedPacket{
		{ft: frame.String, pt: packet.PING, data: "probe"},
		{ft: frame.String, pt: packet.NOOP},
		{ft: frame.String, pt: packet.MESSAGE, data: "hello"},
	}}
	s := newTestSession(t, conn, "polling")

	ft, r, err := s.NextReader()
	require.NoError(t, err)
	require.Equal(t, TEXT, ft)
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "hello", string(b))
	require.NoError(t, r.Close())

	require.Equal(t, []writtenPacket{{ft: frame.String, pt: packet.PONG, data: "probe"}}, conn.written())
	require.False(t, conn.isClosed())
}

func TestSessionNextReaderClosePacket(t *testing.T) {
	conn := &scriptConn{reads: []scriptedPacket{{ft: frame.String, pt: packet.CLOSE}}}
	s := newTestSession(t, conn, "polling")

	_, _, err := s.NextReader()
	require.ErrorIs(t, err, io.EOF)
	require.True(t, conn.isClosed())
}

func TestSessionNextReaderErrors(t *testing.T) {
	readErr := errors.New("read failed")
	conn := &scriptConn{reads: []scriptedPacket{{err: readErr}}}
	s := newTestSession(t, conn, "polling")
	_, _, err := s.NextReader()
	require.ErrorIs(t, err, readErr)
	require.True(t, conn.isClosed())

	writeErr := errors.New("write failed")
	conn = &scriptConn{
		reads:     []scriptedPacket{{ft: frame.String, pt: packet.PING, data: "x"}},
		writerErr: writeErr,
	}
	s = newTestSession(t, conn, "polling")
	_, _, err = s.NextReader()
	require.ErrorIs(t, err, writeErr, "a failed PONG must end the session")
	require.True(t, conn.isClosed())
}

func TestSessionNextWriter(t *testing.T) {
	conn := &scriptConn{}
	s := newTestSession(t, conn, "polling")

	w, err := s.NextWriter(BINARY)
	require.NoError(t, err)
	_, err = w.Write([]byte{1, 2})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.Equal(t, []writtenPacket{{ft: frame.Binary, pt: packet.MESSAGE, data: "\x01\x02"}}, conn.written())
}

func TestSessionInitSession(t *testing.T) {
	conn := &scriptConn{}
	s := newTestSession(t, conn, "polling")
	require.NoError(t, s.InitSession())
	w := conn.written()
	require.Len(t, w, 1)
	require.Equal(t, packet.OPEN, w[0].pt)
	require.Contains(t, w[0].data, `"sid":"sid1"`)
	require.Contains(t, w[0].data, `"upgrades":["websocket"]`)

	writerErr := errors.New("no writer")
	conn = &scriptConn{writerErr: writerErr}
	s = newTestSession(t, conn, "polling")
	require.ErrorIs(t, s.InitSession(), writerErr)
	require.True(t, conn.isClosed())

	closeErr := errors.New("flush failed")
	conn = &scriptConn{wCloseErr: closeErr}
	s = newTestSession(t, conn, "polling")
	require.ErrorIs(t, s.InitSession(), closeErr)
	require.True(t, conn.isClosed())
}

func TestSessionUpgrade(t *testing.T) {
	old := &pausableConn{}
	s := newTestSession(t, old, "polling")

	next := &scriptConn{reads: []scriptedPacket{
		{ft: frame.String, pt: packet.PING, data: "probe"},
		{ft: frame.String, pt: packet.UPGRADE},
	}}
	s.upgrading("websocket", next)

	require.Equal(t, "websocket", s.Transport())
	require.Equal(t, []writtenPacket{{ft: frame.String, pt: packet.PONG, data: "probe"}}, next.written())
	require.True(t, old.isClosed(), "the old connection is closed after a successful upgrade")
	require.False(t, next.isClosed())
	require.Equal(t, 1, old.paused)
	require.Equal(t, 0, old.resumed, "a successful upgrade does not resume the old connection")
}

func TestSessionUpgradeFailures(t *testing.T) {
	cases := map[string]struct {
		reads      []scriptedPacket
		oldPauser  bool
		wantResume int
	}{
		"no probe":         {reads: nil, oldPauser: true},
		"not a ping":       {reads: []scriptedPacket{{ft: frame.String, pt: packet.MESSAGE}}, oldPauser: true},
		"old not pausable": {reads: []scriptedPacket{{ft: frame.String, pt: packet.PING, data: "probe"}}},
		"no upgrade packet": {
			reads:      []scriptedPacket{{ft: frame.String, pt: packet.PING, data: "probe"}},
			oldPauser:  true,
			wantResume: 1,
		},
		"wrong packet after probe": {
			reads: []scriptedPacket{
				{ft: frame.String, pt: packet.PING, data: "probe"},
				{ft: frame.String, pt: packet.MESSAGE},
			},
			oldPauser:  true,
			wantResume: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var old transport.Conn = &scriptConn{}
			p := &pausableConn{}
			if tc.oldPauser {
				old = p
			}
			s := newTestSession(t, old, "polling")
			next := &scriptConn{reads: tc.reads}

			s.upgrading("websocket", next)

			require.Equal(t, "polling", s.Transport(), "a failed upgrade keeps the old transport")
			require.True(t, next.isClosed(), "the candidate connection is closed")
			require.Equal(t, tc.wantResume, p.resumed)
		})
	}
}

func TestSessionUpgradePongWriteFails(t *testing.T) {
	s := newTestSession(t, &pausableConn{}, "polling")
	next := &scriptConn{
		reads:     []scriptedPacket{{ft: frame.String, pt: packet.PING, data: "probe"}},
		writerErr: errors.New("no writer"),
	}
	s.upgrading("websocket", next)
	require.Equal(t, "polling", s.Transport())
	require.True(t, next.isClosed())
}

func transportParams(sid string) transport.ConnParameters {
	return transport.ConnParameters{SID: sid}
}

// handlerConn is a scriptConn that also serves HTTP, as the polling
// transport does.
type handlerConn struct {
	scriptConn
	served int
}

func (c *handlerConn) ServeHTTP(http.ResponseWriter, *http.Request) {
	c.mu.Lock()
	c.served++
	c.mu.Unlock()
}

func TestSessionServeHTTPDelegatesToConn(t *testing.T) {
	conn := &handlerConn{}
	s := newTestSession(t, conn, "polling")
	s.ServeHTTP(nil, nil)
	require.Equal(t, 1, conn.served)

	// A connection that is not an http.Handler is ignored.
	newTestSession(t, &scriptConn{}, "websocket").ServeHTTP(nil, nil)
}

func TestSessionUpgradeRunsAsync(t *testing.T) {
	s := newTestSession(t, &pausableConn{}, "polling")
	next := &scriptConn{reads: []scriptedPacket{
		{ft: frame.String, pt: packet.PING, data: "probe"},
		{ft: frame.String, pt: packet.UPGRADE},
	}}
	s.Upgrade("websocket", next)
	require.Eventually(t, func() bool { return s.Transport() == "websocket" },
		5*time.Second, 5*time.Millisecond)
}

// closingConn is a pausable transport.Conn whose NextReader and NextWriter
// block until Close and then fail with io.EOF, as a closed polling payload
// does. entered is signalled each time a reader or writer starts waiting.
type closingConn struct {
	pausableConn
	once    sync.Once
	done    chan struct{}
	entered chan struct{}
}

func newClosingConn() *closingConn {
	return &closingConn{done: make(chan struct{}), entered: make(chan struct{}, 2)}
}

func (c *closingConn) NextReader() (frame.Type, packet.Type, io.ReadCloser, error) {
	c.entered <- struct{}{}
	<-c.done
	return 0, 0, nil, io.EOF
}

func (c *closingConn) NextWriter(frame.Type, packet.Type) (io.WriteCloser, error) {
	c.entered <- struct{}{}
	<-c.done
	return nil, io.EOF
}

func (c *closingConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

// TestSessionReadSurvivesUpgrade checks that a read blocked on the old
// connection when an upgrade completes continues on the new connection
// instead of failing: closing the replaced connection is not an error of
// the session.
func TestSessionReadSurvivesUpgrade(t *testing.T) {
	old := newClosingConn()
	s := newTestSession(t, old, "polling")

	type result struct {
		data string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		_, r, err := s.NextReader()
		if err != nil {
			got <- result{err: err}
			return
		}
		b, _ := io.ReadAll(r)
		_ = r.Close()
		got <- result{data: string(b)}
	}()
	<-old.entered

	next := &scriptConn{reads: []scriptedPacket{{ft: frame.String, pt: packet.MESSAGE, data: "after upgrade"}}}
	s.switchTransport("websocket", next)
	require.NoError(t, old.Close())

	select {
	case r := <-got:
		require.NoError(t, r.err)
		require.Equal(t, "after upgrade", r.data)
	case <-time.After(5 * time.Second):
		t.Fatal("NextReader did not return")
	}
	require.False(t, next.isClosed(), "the new connection must stay open")
}

// TestSessionWriteSurvivesUpgrade is the same for a writer.
func TestSessionWriteSurvivesUpgrade(t *testing.T) {
	old := newClosingConn()
	s := newTestSession(t, old, "polling")

	errs := make(chan error, 1)
	go func() {
		w, err := s.NextWriter(TEXT)
		if err == nil {
			_, _ = w.Write([]byte("x"))
			err = w.Close()
		}
		errs <- err
	}()
	<-old.entered

	next := &scriptConn{}
	s.switchTransport("websocket", next)
	require.NoError(t, old.Close())

	select {
	case err := <-errs:
		require.NoError(t, err)
		require.Equal(t, []writtenPacket{{ft: frame.String, pt: packet.MESSAGE, data: "x"}}, next.written())
	case <-time.After(5 * time.Second):
		t.Fatal("NextWriter did not return")
	}
}

// heldConn is a transport.Conn whose NextReader and NextWriter block until
// release is closed, independently of Close, and then fail with io.EOF. It
// models a polling payload whose waiting reader is woken after Close returns.
type heldConn struct {
	pausableConn
	release chan struct{}
	entered chan struct{}
}

func newHeldConn() *heldConn {
	return &heldConn{release: make(chan struct{}), entered: make(chan struct{}, 2)}
}

func (c *heldConn) NextReader() (frame.Type, packet.Type, io.ReadCloser, error) {
	c.entered <- struct{}{}
	<-c.release
	return 0, 0, nil, io.EOF
}

func (c *heldConn) NextWriter(frame.Type, packet.Type) (io.WriteCloser, error) {
	c.entered <- struct{}{}
	<-c.release
	return nil, io.EOF
}

// TestSessionClosedBeforeUpgradeCompletes checks the order: an operation is
// blocked on the old connection, the session is closed, then an upgrade
// completes, then the old connection fails. The session must stay closed:
// the operation fails and the incoming connection is closed, not installed.
func TestSessionClosedBeforeUpgradeCompletes(t *testing.T) {
	for _, op := range []string{"read", "write"} {
		t.Run(op, func(t *testing.T) {
			old := newHeldConn()
			s := newTestSession(t, old, "polling")

			errs := make(chan error, 1)
			go func() {
				if op == "read" {
					_, _, err := s.NextReader()
					errs <- err
					return
				}
				_, err := s.NextWriter(TEXT)
				errs <- err
			}()
			<-old.entered

			require.NoError(t, s.Close())
			next := &scriptConn{reads: []scriptedPacket{{ft: frame.String, pt: packet.MESSAGE, data: "late"}}}
			s.switchTransport("websocket", next)
			close(old.release)

			select {
			case err := <-errs:
				require.Error(t, err, "%s must fail after Session.Close", op)
			case <-time.After(5 * time.Second):
				t.Fatalf("%s did not return", op)
			}
			require.True(t, next.isClosed(), "a connection arriving after Close must be closed")
			require.Equal(t, "polling", s.Transport(), "a closed session is not upgraded")
		})
	}
}
