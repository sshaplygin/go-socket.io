package websocket

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// closeFixture is a server-side transport connection with a raw gorilla peer.
type closeFixture struct {
	server transport.Conn
	peer   *gorilla.Conn
}

func newCloseFixture(t *testing.T, tran *Transport, dialer *gorilla.Dialer) *closeFixture {
	t.Helper()
	accepted := make(chan transport.Conn, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := tran.Accept(w, r)
		if err == nil {
			accepted <- c
		}
	}))
	t.Cleanup(ts.Close)
	u, err := url.Parse(ts.URL)
	require.NoError(t, err)
	u.Scheme = "ws"
	peer, _, err := dialer.Dial(u.String(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	f := &closeFixture{peer: peer}
	select {
	case f.server = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("no accepted connection")
	}
	t.Cleanup(func() { _ = f.server.Close() })
	return f
}

// readClose reads from the peer until it fails and returns the failure.
func readClose(t *testing.T, peer *gorilla.Conn) error {
	t.Helper()
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(10*time.Second)))
	for {
		if _, _, err := peer.ReadMessage(); err != nil {
			return err
		}
	}
}

func requireCloseCode(t *testing.T, err error, code int) {
	t.Helper()
	var closeErr *gorilla.CloseError
	require.ErrorAs(t, err, &closeErr, "the peer must read a websocket close frame")
	require.Equal(t, code, closeErr.Code, "%v", err)
}

// A server-side Close tells the peer with a normal closure frame, so that it can
// tell an orderly stop from a dropped connection (close code 1006).
func TestServerCloseSendsNormalClosureFrame(t *testing.T) {
	for _, tc := range []struct {
		name string
		tran *Transport
		do   func(t *testing.T, f *closeFixture)
	}{
		{name: "idle", tran: &Transport{}},
		{name: "compression", tran: &Transport{EnableCompression: true, CompressionThreshold: 1}},
		{name: "after a message", tran: &Transport{}, do: func(t *testing.T, f *closeFixture) {
			w, err := f.server.NextWriter(eioparser.String, eioparser.MESSAGE)
			require.NoError(t, err)
			_, err = w.Write([]byte("hello"))
			require.NoError(t, err)
			require.NoError(t, w.Close())
			_, data, err := f.peer.ReadMessage()
			require.NoError(t, err)
			require.Equal(t, "4hello", string(data))
		}},
		{name: "writer open", tran: &Transport{}, do: func(t *testing.T, f *closeFixture) {
			// An unfinished message holds the transport's write lock. Close must
			// neither wait for the writer nor skip the frame.
			w, err := f.server.NextWriter(eioparser.String, eioparser.MESSAGE)
			require.NoError(t, err)
			_, err = w.Write([]byte(strings.Repeat("x", 1<<16)))
			require.NoError(t, err)
			t.Cleanup(func() { _ = w.Close() })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dialer := &gorilla.Dialer{EnableCompression: tc.tran.EnableCompression}
			f := newCloseFixture(t, tc.tran, dialer)
			if tc.do != nil {
				tc.do(t, f)
			}
			done := make(chan error, 1)
			go func() { done <- f.server.Close() }()
			requireCloseCode(t, readClose(t, f.peer), gorilla.CloseNormalClosure)
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(10 * time.Second):
				t.Fatal("Close did not return")
			}
			require.NoError(t, f.server.Close(), "second Close")
		})
	}
}

// Abort severs the connection without a frame: the peer sees 1006.
func TestServerAbortSendsNoCloseFrame(t *testing.T) {
	f := newCloseFixture(t, &Transport{}, &gorilla.Dialer{})
	aborter, ok := f.server.(interface{ Abort() error })
	require.True(t, ok, "the websocket connection must support an immediate Abort")
	require.NoError(t, aborter.Abort())
	requireCloseCode(t, readClose(t, f.peer), gorilla.CloseAbnormalClosure)
	require.NoError(t, f.server.Close(), "Close after Abort")
}

// A peer that stopped reading, with its receive window full, cannot hold Close
// beyond the close frame's write deadline, and Close unblocks the writer.
func TestServerCloseStalledPeerIsBounded(t *testing.T) {
	f := newCloseFixture(t, &Transport{}, &gorilla.Dialer{})
	var written atomic.Int64
	writeDone := make(chan error, 1)
	go func() {
		w, err := f.server.NextWriter(eioparser.String, eioparser.MESSAGE)
		if err != nil {
			writeDone <- err
			return
		}
		chunk := []byte(strings.Repeat("x", 32<<10))
		for {
			n, err := w.Write(chunk)
			written.Add(int64(n))
			if err != nil {
				writeDone <- err
				return
			}
		}
	}()
	// The peer never reads: the writer stops making progress once the socket
	// buffers are full. Close is called after that point.
	var last int64 = -1
	for stable := 0; stable < 5; {
		time.Sleep(20 * time.Millisecond)
		if now := written.Load(); now == last && now > 0 {
			stable++
		} else {
			stable, last = 0, now
		}
	}

	start := time.Now()
	closed := make(chan error, 1)
	go func() { closed <- f.server.Close() }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close is not bounded by a stalled peer")
	}
	require.Less(t, time.Since(start), 5*time.Second)
	select {
	case err := <-writeDone:
		require.Error(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not release the blocked writer")
	}
}
