package engineio

import (
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// closeFramePeer is a raw websocket peer of one engine.io session.
type closeFramePeer struct {
	srv  *Server
	conn Conn
	ws   *gorilla.Conn
}

func requirePeerClose(t *testing.T, ws *gorilla.Conn, code int) {
	t.Helper()
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(10*time.Second)))
	for {
		_, _, err := ws.ReadMessage()
		if err == nil {
			continue
		}
		var closeErr *gorilla.CloseError
		require.ErrorAs(t, err, &closeErr, "the peer must read a websocket close frame")
		require.Equal(t, code, closeErr.Code, "%v", err)
		return
	}
}

// dialCloseFramePeer opens a websocket-first session and consumes its OPEN packet.
func dialCloseFramePeer(t *testing.T, compression bool, opts ...Option) *closeFramePeer {
	t.Helper()
	opts = append([]Option{WithPerMessageDeflate(compression), WithCompressionThreshold(1)}, opts...)
	p := &closeFramePeer{srv: NewServer(opts...)}
	ts := httptest.NewServer(p.srv)
	t.Cleanup(func() { _ = p.srv.Close(); ts.Close() })
	accepted := make(chan Conn, 1)
	go func() {
		if c, err := p.srv.Accept(); err == nil {
			accepted <- c
		}
	}()
	dialer := &gorilla.Dialer{EnableCompression: compression}
	ws, _, err := dialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	p.ws = ws
	_, data, err := ws.ReadMessage()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(data), "0"), "OPEN: %s", data)
	select {
	case p.conn = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("OPEN was not accepted")
	}
	return p
}

// closers are the server-side ways to end a session in an orderly way.
var closeFrameClosers = map[string]func(p *closeFramePeer) error{
	"Conn.Close":   func(p *closeFramePeer) error { return p.conn.Close() },
	"Server.Close": func(p *closeFramePeer) error { return p.srv.Close() },
}

// closeAsync runs closer while the peer reads: Close waits briefly for the
// peer's reply to its close frame.
func closeAsync(closer func(*closeFramePeer) error, p *closeFramePeer) <-chan error {
	done := make(chan error, 1)
	go func() { done <- closer(p) }()
	return done
}

// A server that closes a websocket session tells the client with a normal
// closure frame, as engine.io 3.6.2 does, rather than dropping the TCP connection
// (close code 1006).
func TestServerCloseSendsNormalClosureFrameToIdleClient(t *testing.T) {
	for name, closer := range closeFrameClosers {
		for _, compression := range []bool{false, true} {
			t.Run(name+"/compression="+map[bool]string{false: "off", true: "on"}[compression], func(t *testing.T) {
				p := dialCloseFramePeer(t, compression)
				if compression {
					// A compressed message first, so that the connection has used
					// permessage-deflate before it closes.
					require.NoError(t, <-parityWrite(p.conn, strings.Repeat("compress me ", 100)))
					_, _, err := p.ws.ReadMessage()
					require.NoError(t, err)
				}
				closed := closeAsync(closer, p)
				requirePeerClose(t, p.ws, gorilla.CloseNormalClosure)
				require.NoError(t, <-closed)
			})
		}
	}
}

// A client in the middle of a message does not keep the close frame from being
// sent, and the half-received message ends with the session. The client's first
// fragment is read completely before the close, so that no unread byte makes the
// kernel reset the connection and discard the frame.
func TestServerCloseSendsNormalClosureFrameToClientMidWrite(t *testing.T) {
	for name, closer := range closeFrameClosers {
		t.Run(name, func(t *testing.T) {
			p := dialCloseFramePeer(t, false)
			w, err := p.ws.NextWriter(gorilla.TextMessage)
			require.NoError(t, err)
			// One byte more than the 4096-byte write buffer: a nonfinal fragment
			// of 4096 bytes is sent and one byte stays buffered.
			_, err = w.Write([]byte("4" + strings.Repeat("x", 4096)))
			require.NoError(t, err)
			_, r, err := p.conn.NextReader()
			require.NoError(t, err)
			_, err = io.ReadFull(r, make([]byte, 4095))
			require.NoError(t, err)

			// The application keeps reading the unfinished message.
			read := make(chan error, 1)
			go func() { _, err := io.ReadAll(r); read <- err }()
			closed := closeAsync(closer, p)
			requirePeerClose(t, p.ws, gorilla.CloseNormalClosure)
			require.NoError(t, <-closed)
			select {
			case err := <-read:
				require.Error(t, err, "the unfinished message must not complete")
			case <-time.After(10 * time.Second):
				t.Fatal("the message reader did not end")
			}
		})
	}
}

// A session upgraded from polling closes its websocket with the same frame.
func TestServerCloseSendsNormalClosureFrameAfterUpgrade(t *testing.T) {
	for _, name := range []string{"Conn.Close", "Server.Close"} {
		t.Run(name, func(t *testing.T) {
			upgraded := make(chan struct{}, 1)
			hooks := &Hooks{Upgrade: func(Conn, string) { upgraded <- struct{}{} }}
			f := newParityWire(t, []Option{WithHooks(hooks)})
			_ = parityReader(f.conn)
			ws := f.probe(t)
			require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte("5")))
			select {
			case <-upgraded:
			case <-time.After(5 * time.Second):
				t.Fatal("the session did not upgrade")
			}
			require.Equal(t, "websocket", f.conn.Transport())
			closed := make(chan error, 1)
			go func() {
				if name == "Conn.Close" {
					closed <- f.conn.Close()
				} else {
					closed <- f.srv.Close()
				}
			}()
			requirePeerClose(t, ws, gorilla.CloseNormalClosure)
			require.NoError(t, <-closed)
		})
	}
}

// The server also closes with the frame when the client sends CLOSE and when
// the session times out for lack of pings.
func TestSessionEndsWithNormalClosureFrame(t *testing.T) {
	t.Run("CLOSE packet", func(t *testing.T) {
		p := dialCloseFramePeer(t, false)
		_ = parityReader(p.conn)
		require.NoError(t, p.ws.WriteMessage(gorilla.TextMessage, []byte("1")))
		requirePeerClose(t, p.ws, gorilla.CloseNormalClosure)
	})
	t.Run("ping timeout", func(t *testing.T) {
		// The heartbeat window starts at session creation and also bounds the OPEN write
		// that dialCloseFramePeer reads, so it must outlast a loaded scheduler.
		p := dialCloseFramePeer(t, false, WithPingInterval(20*time.Millisecond), WithPingTimeout(time.Second))
		_ = parityReader(p.conn)
		requirePeerClose(t, p.ws, gorilla.CloseNormalClosure)
	})
}

// Abort stays immediate: the client sees the connection drop (1006) and no frame.
func TestAbortSendsNoCloseFrame(t *testing.T) {
	p := dialCloseFramePeer(t, false)
	require.NoError(t, Abort(p.conn))
	requirePeerClose(t, p.ws, gorilla.CloseAbnormalClosure)
}

// A client that stopped reading, with its receive window full, delays a close by at
// most the close frame's write deadline, and the closed session frees its writer.
func TestServerCloseStalledClientIsBounded(t *testing.T) {
	for name, closer := range closeFrameClosers {
		t.Run(name, func(t *testing.T) {
			p := dialCloseFramePeer(t, false)
			var written atomic.Int64
			writeDone := make(chan error, 1)
			go func() {
				w, err := p.conn.NextWriter(eioparser.String)
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
			// The client never reads, so the writer stalls once the socket buffers
			// are full. Close runs after that point.
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
			go func() { closed <- closer(p) }()
			select {
			case err := <-closed:
				t.Logf("close returned: %v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("close is not bounded by a stalled client")
			}
			require.Less(t, time.Since(start), 5*time.Second)
			select {
			case err := <-writeDone:
				require.Error(t, err)
			case <-time.After(10 * time.Second):
				t.Fatal("close did not release the blocked writer")
			}
		})
	}
}
