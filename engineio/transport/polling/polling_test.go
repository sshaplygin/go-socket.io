package polling

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

var tests = []struct {
	ft   eioparser.FrameType
	pt   eioparser.PacketType
	data []byte
}{
	{eioparser.String, eioparser.OPEN, []byte{}},
	{eioparser.String, eioparser.MESSAGE, []byte("hello")},
	{eioparser.Binary, eioparser.MESSAGE, []byte{1, 2, 3, 4}},
}

func TestPollingBinary(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	var scValue atomic.Value

	pollingTransport := Default
	should.Equal("polling", pollingTransport.Name())

	conn := make(chan transport.Conn, 1)
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Eio-Test", "server")
		c := scValue.Load()
		if c == nil {
			co, err := pollingTransport.Accept(w, r)
			should.Nil(err)
			scValue.Store(co)
			c = co
			conn <- co
		}
		c.(http.Handler).ServeHTTP(w, r)
	}

	httpSvr := httptest.NewServer(http.HandlerFunc(handler))
	defer httpSvr.Close()

	u, err := url.Parse(httpSvr.URL)
	should.Nil(err)

	dialU := *u

	header := make(http.Header)
	header.Set("X-Eio-Test", "client")
	cc, err := pollingTransport.Dial(&dialU, header)
	should.Nil(err)

	cc.(*clientConn).Resume()
	defer func() {
		must.NoError(cc.Close())
	}()

	clientDone := startClientEcho(cc)
	sc := acceptServerConn(t, conn, clientDone)
	defer func() {
		must.NoError(sc.Close())
	}()

	finishExchange(t, sc, clientDone)

	should.Equal(sc.LocalAddr(), cc.RemoteAddr())
	should.Empty(cc.LocalAddr().String())
	should.NotEmpty(sc.RemoteAddr().String())
	should.Equal("server", cc.RemoteHeader().Get("X-Eio-Test"))
	should.Equal("client", sc.RemoteHeader().Get("X-Eio-Test"))
}

func TestPollingString(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	var scValue atomic.Value

	pollingTransport := Default
	conn := make(chan transport.Conn, 1)
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Eio-Test", "server")
		c := scValue.Load()
		if c == nil {
			co, err := pollingTransport.Accept(w, r)
			should.Nil(err)
			scValue.Store(co)
			c = co
			conn <- co
		}
		c.(http.Handler).ServeHTTP(w, r)
	}
	httpSvr := httptest.NewServer(http.HandlerFunc(handler))
	defer httpSvr.Close()

	u, err := url.Parse(httpSvr.URL)
	should.Nil(err)

	query := u.Query()
	query.Set("b64", "1")
	u.RawQuery = query.Encode()

	dialU := *u
	header := make(http.Header)
	header.Set("X-Eio-Test", "client")
	cc, err := pollingTransport.Dial(&dialU, header)
	should.Nil(err)

	cc.(*clientConn).Resume()
	defer func() {
		must.NoError(cc.Close())
	}()

	clientDone := startClientEcho(cc)
	sc := acceptServerConn(t, conn, clientDone)
	defer func() {
		must.NoError(sc.Close())
	}()

	should.Equal(sc.LocalAddr(), cc.RemoteAddr())
	should.Equal("tcp", sc.LocalAddr().Network())
	should.Empty(cc.LocalAddr().String())
	should.NotEmpty(sc.RemoteAddr().String())

	finishExchange(t, sc, clientDone)

	should.Equal("server", cc.RemoteHeader().Get("X-Eio-Test"))
	should.Equal("client", sc.RemoteHeader().Get("X-Eio-Test"))
}

// TestPollingClientRequestFailure reproduces the TestPollingBinary crash from
// a run under ephemeral-port exhaustion. A failed client request makes
// NextReader return that error with a nil reader; the exchange must stop on the
// error instead of reading from the nil reader.
func TestPollingClientRequestFailure(t *testing.T) {
	must := require.New(t)

	dialErr := errors.New("dial tcp: connect: can't assign requested address")
	failing := &Transport{Client: &http.Client{Transport: dialOpenRoundTripper(
		func(*http.Request) (*http.Response, error) { return nil, dialErr },
	)}}
	u, err := url.Parse("http://127.0.0.1/")
	must.NoError(err)
	cc, err := failing.Dial(u, nil)
	must.NoError(err)
	defer func() {
		must.NoError(cc.Close())
	}()
	cc.(*clientConn).Resume()

	_, _, r, err := cc.NextReader()
	must.Nil(r)
	var opErr *OpError
	must.ErrorAs(err, &opErr)
	must.Equal("get", opErr.Op)
	must.ErrorIs(opErr.Err, dialErr)

	must.ErrorIs(echoPackets(cc), err)
}

// startClientEcho runs echoPackets on c in a new goroutine and reports its
// result.
func startClientEcho(c transport.Conn) <-chan error {
	done := make(chan error, 1)
	go func() { done <- echoPackets(c) }()
	return done
}

// acceptServerConn returns the connection accepted by the test server. It
// fails the test if the client half ends first, which happens when the
// client's first request fails and the server never sees it.
func acceptServerConn(t *testing.T, conn <-chan transport.Conn, clientDone <-chan error) transport.Conn {
	t.Helper()
	select {
	case sc := <-conn:
		return sc
	case err := <-clientDone:
		require.NoError(t, err, "client")
		require.FailNow(t, "client finished before the server accepted a connection")
		return nil
	}
}

// finishExchange runs sendAndExpectEcho on sc and waits for both halves. The
// first error fails the test without waiting for the other half: the callers'
// deferred Close calls end its blocked operations.
func finishExchange(t *testing.T, sc transport.Conn, clientDone <-chan error) {
	t.Helper()
	serverDone := make(chan error, 1)
	go func() { serverDone <- sendAndExpectEcho(sc) }()
	for range 2 {
		select {
		case err := <-clientDone:
			require.NoError(t, err, "client")
			clientDone = nil
		case err := <-serverDone:
			require.NoError(t, err, "server")
			serverDone = nil
		}
	}
}

// echoPackets is the client half of the exchange: it reads each packet of
// tests and writes it back, and returns the first error.
func echoPackets(c transport.Conn) error {
	for _, test := range tests {
		if err := receivePacket(c, test.ft, test.pt, test.data); err != nil {
			return err
		}
		if err := sendPacket(c, test.ft, test.pt, test.data); err != nil {
			return err
		}
	}
	return nil
}

// sendAndExpectEcho is the server half of the exchange: it writes each packet
// of tests and reads the client's echo, and returns the first error.
func sendAndExpectEcho(c transport.Conn) error {
	for _, test := range tests {
		if err := sendPacket(c, test.ft, test.pt, test.data); err != nil {
			return err
		}
		if err := receivePacket(c, test.ft, test.pt, test.data); err != nil {
			return err
		}
	}
	return nil
}

func sendPacket(c transport.Conn, ft eioparser.FrameType, pt eioparser.PacketType, data []byte) error {
	w, err := c.NextWriter(ft, pt)
	if err != nil {
		return fmt.Errorf("next writer: %w", err)
	}
	if _, err = w.Write(data); err != nil {
		_ = w.Close()
		return fmt.Errorf("write packet: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("close writer: %w", err)
	}
	return nil
}

// receivePacket reads one packet and compares it with the expected one. A
// NextReader error is returned before the reader is used, because the reader
// is nil when NextReader fails.
func receivePacket(c transport.Conn, ft eioparser.FrameType, pt eioparser.PacketType, data []byte) error {
	gotFT, gotPT, r, err := c.NextReader()
	if err != nil {
		return fmt.Errorf("next reader: %w", err)
	}
	b, err := io.ReadAll(r)
	if closeErr := r.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("read packet: %w", err)
	}
	if gotFT != ft || gotPT != pt || !bytes.Equal(b, data) {
		return fmt.Errorf("got packet %v %v %q, want %v %v %q", gotFT, gotPT, b, ft, pt, data)
	}
	return nil
}
