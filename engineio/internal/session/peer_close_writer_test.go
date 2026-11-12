package session

import (
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// A frame writer that fails, in Write or Close, with websocket.ErrCloseSent after
// the reply to the peer's orderly close frame observes "transport close". The frame
// is consumed through the transport, so the session's reader never sees it. "Write"
// writes more than gorilla's 4096-byte write buffer: that Write flushes and fails.
func TestFrameWriterObservesPeerClose(t *testing.T) {
	for _, failing := range []string{"Write", "Close"} {
		t.Run(failing, func(t *testing.T) {
			conn, peer := acceptProbe(t)
			s := newTestSession(t, conn, "websocket")
			t.Cleanup(func() { _ = s.Abort() })
			w, err := s.NextWriter(eioparser.String)
			require.NoError(t, err)
			_, err = w.Write([]byte("x")) // buffered: the message has started
			require.NoError(t, err)
			msg := gorilla.FormatCloseMessage(gorilla.CloseNormalClosure, "")
			require.NoError(t, peer.WriteControl(gorilla.CloseMessage, msg, time.Now().Add(time.Second)))
			_, _, _, err = conn.NextReader() // replies to the frame
			require.Error(t, err)
			if failing == "Write" {
				_, err = w.Write([]byte(strings.Repeat("x", 3*4096)))
				require.ErrorIs(t, err, gorilla.ErrCloseSent)
			}
			if err = w.Close(); failing == "Close" {
				require.ErrorIs(t, err, gorilla.ErrCloseSent)
			}
			require.Equal(t, ReasonTransportClose, s.closeError().Reason)
		})
	}
}
