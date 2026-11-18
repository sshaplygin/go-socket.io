package client

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// A websocket close frame from the server is an orderly end, which
// engine.io-client 3.5.6 reports as "transport close": the websocket onclose
// handler closes the transport without an error. Frames that signal a failure
// (message too big, internal error) remain transport errors.
// Not parallel: it sets slog.Default.
func TestClientReportsServerCloseFrame(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload []byte
		reason  CloseReason
	}{
		{"normal closure", gorilla.FormatCloseMessage(gorilla.CloseNormalClosure, ""), ReasonTransportClose},
		{"going away", gorilla.FormatCloseMessage(gorilla.CloseGoingAway, ""), ReasonTransportClose},
		{"no status", nil, ReasonTransportClose},
		{"message too big", gorilla.FormatCloseMessage(gorilla.CloseMessageTooBig, ""), ReasonTransportError},
		{"internal error", gorilla.FormatCloseMessage(gorilla.CloseInternalServerErr, ""), ReasonTransportError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if runLogTestInSubprocess(t) {
				return
			}
			rec := newRecorder()
			setDefault(t, rec)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer func() { _ = c.Close() }()
				open := `0{"sid":"x","upgrades":[],"pingInterval":50,"pingTimeout":5000}`
				_ = c.WriteMessage(gorilla.TextMessage, []byte(open))
				_ = c.WriteControl(gorilla.CloseMessage, tc.payload, time.Now().Add(time.Second))
				for err == nil {
					_, _, err = c.NextReader()
				}
			}))
			defer ts.Close()

			cl, err := (&Dialer{Transports: []transport.Transport{websocket.Default}}).Dial(ts.URL, nil)
			require.NoError(t, err)
			defer func() { _ = cl.Close() }()

			err = readAll(cl)
			var closed *CloseError
			require.ErrorAs(t, err, &closed)
			require.Equal(t, tc.reason, closed.Reason, "%v", err)
			if tc.reason == ReasonTransportClose {
				require.ErrorIs(t, err, io.EOF)
			} else {
				var closeErr *gorilla.CloseError
				require.ErrorAs(t, err, &closeErr)
			}
			for _, m := range rec.find("") {
				assert.Equal(t, slog.LevelDebug.String(), m["level"], "%v", m)
			}
		})
	}
}
