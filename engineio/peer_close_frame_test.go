package engineio

import (
	"io"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

func peerCloseFrame(payload []byte) func(*gorilla.Conn) error {
	return func(ws *gorilla.Conn) error {
		return ws.WriteControl(gorilla.CloseMessage, payload, time.Now().Add(time.Second))
	}
}

// peerCloses are the ways a client ends its websocket; the first three are orderly.
var peerCloses = []struct {
	name   string
	close  func(*gorilla.Conn) error
	reason CloseReason
}{
	{"normal closure", peerCloseFrame(gorilla.FormatCloseMessage(gorilla.CloseNormalClosure, "")), ReasonTransportClose},
	{"going away", peerCloseFrame(gorilla.FormatCloseMessage(gorilla.CloseGoingAway, "")), ReasonTransportClose},
	{"no status", peerCloseFrame(nil), ReasonTransportClose},
	{"internal error", peerCloseFrame(gorilla.FormatCloseMessage(gorilla.CloseInternalServerErr, "")), ReasonTransportError},
	{"policy violation", peerCloseFrame(gorilla.FormatCloseMessage(gorilla.ClosePolicyViolation, "")), ReasonTransportError},
	{"dropped", func(ws *gorilla.Conn) error { return ws.UnderlyingConn().Close() }, ReasonTransportError},
}

// A close frame from the client with status 1000, 1001 or none (1005) ends the
// session with "transport close", as engine.io 3.6.2 reports it: between
// packets, in the middle of a MESSAGE or a PING and after an upgrade. Other
// statuses and a connection dropped without a frame stay "transport error".
func TestPeerCloseFrameReason(t *testing.T) {
	for _, tc := range peerCloses {
		for _, at := range []string{"idle", "mid-message", "mid-ping", "upgraded"} {
			t.Run(tc.name+"/"+at, func(t *testing.T) {
				var conn Conn
				var ws *gorilla.Conn
				if at == "upgraded" {
					f := newParityWire(t, nil)
					conn, ws = f.conn, f.probe(t)
					require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte("5")))
					require.Eventually(t, conn.Upgraded, 5*time.Second, time.Millisecond)
				} else {
					p := dialCloseFramePeer(t, false)
					conn, ws = p.conn, p.ws
				}
				var r io.ReadCloser
				if packet, ok := map[string]string{"mid-message": "4", "mid-ping": "2"}[at]; ok {
					w, err := ws.NextWriter(gorilla.TextMessage)
					require.NoError(t, err)
					// One byte more than the 4096-byte write buffer flushes a
					// nonfinal fragment; the frame that follows it is the close.
					_, err = w.Write([]byte(packet + strings.Repeat("x", 4096)))
					require.NoError(t, err)
					if packet == "4" {
						_, r, err = conn.NextReader()
						require.NoError(t, err)
					}
				}
				require.NoError(t, tc.close(ws))
				if r != nil {
					_, err := io.Copy(io.Discard, r)
					require.Error(t, err)
					_ = r.Close()
				}
				err := readAll(conn)
				var closed *CloseError
				require.ErrorAs(t, err, &closed)
				require.Equal(t, tc.reason, closed.Reason, "%v", err)
				if tc.reason == ReasonTransportClose {
					require.ErrorIs(t, err, io.EOF)
				}
			})
		}
	}
}

// The reason is the same with a writer running on another goroutine, whose write
// can fail with websocket.ErrCloseSent once the peer's close frame is answered.
// One server serves every session, so that repeated runs do not exhaust ports.
func TestPeerCloseFrameReasonWhileWriting(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("the write must run in parallel with the reader that receives the close frame")
	}
	const sessions = 200
	srv := NewServer()
	ts := httptest.NewServer(srv)
	defer func() { _ = srv.Close(); ts.Close() }()
	reasons := map[CloseReason]int{}
	for i := 0; i < sessions; i++ {
		ws, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
		require.NoError(t, err)
		conn, err := srv.Accept()
		require.NoError(t, err)
		writing := make(chan struct{})
		go func() {
			defer close(writing)
			for w, err := conn.NextWriter(eioparser.String); err == nil; w, err = conn.NextWriter(eioparser.String) {
				if _, err = w.Write([]byte("tick")); w.Close() != nil || err != nil {
					return
				}
			}
		}()
		for k := 0; k < 2; k++ { // OPEN and a tick: the writer is running
			_, _, err = ws.ReadMessage()
			require.NoError(t, err)
		}
		go func() { _, _ = io.Copy(io.Discard, ws.UnderlyingConn()) }()
		require.NoError(t, peerCloses[i%3].close(ws))
		var closed *CloseError
		require.ErrorAs(t, readAll(conn), &closed)
		<-writing
		reasons[closed.Reason]++
		_ = ws.Close()
	}
	require.Equal(t, map[CloseReason]int{ReasonTransportClose: sessions}, reasons)
}
