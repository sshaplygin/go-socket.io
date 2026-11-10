package engineio

import (
	"io"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// orderlyClosePayloads are the close frames with status 1000, 1001 and none.
var orderlyClosePayloads = [][]byte{
	gorilla.FormatCloseMessage(gorilla.CloseNormalClosure, ""),
	gorilla.FormatCloseMessage(gorilla.CloseGoingAway, ""), nil,
}

func peerCloseFrame(payload []byte) func(*gorilla.Conn) error {
	return func(ws *gorilla.Conn) error {
		return ws.WriteControl(gorilla.CloseMessage, payload, time.Now().Add(time.Second))
	}
}

// A close frame from the client with status 1000, 1001 or none (1005) ends the
// session with "transport close", as engine.io 3.6.2 reports it: between
// packets, in the middle of a MESSAGE or a PING and after an upgrade. Other
// statuses and a connection dropped without a frame stay "transport error".
func TestPeerCloseFrameReason(t *testing.T) {
	for _, tc := range []struct {
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
	} {
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

// A server write that fails because the peer's close frame was answered first
// (websocket.ErrCloseSent) does not turn the orderly close into a transport
// error: the reason is the same with a writer running on another goroutine.
func TestPeerCloseFrameReasonWhileWriting(t *testing.T) {
	const sessions = 300
	reasons := map[CloseReason]int{}
	for i := 0; i < sessions; i++ {
		p := dialCloseFramePeer(t, false)
		writing := make(chan struct{})
		go func() {
			defer close(writing)
			for {
				w, err := p.conn.NextWriter(eioparser.String)
				if err != nil {
					return
				}
				if _, err = w.Write([]byte("tick")); w.Close() != nil || err != nil {
					return
				}
			}
		}()
		_, _, err := p.ws.ReadMessage() // the writer is running
		require.NoError(t, err)
		go func() {
			for err == nil {
				_, _, err = p.ws.NextReader()
			}
		}()
		require.NoError(t, peerCloseFrame(orderlyClosePayloads[i%3])(p.ws))
		var closed *CloseError
		require.ErrorAs(t, readAll(p.conn), &closed)
		<-writing
		reasons[closed.Reason]++
		_ = p.srv.Close()
	}
	require.Equal(t, map[CloseReason]int{ReasonTransportClose: sessions}, reasons)
}
