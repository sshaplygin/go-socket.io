package socketio

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

// A packet that cannot be decoded is reported to OnError, on a connection that is still
// connected, before the library closes it with forced close; the close discards the
// queue, so an Emit from that OnError call may not be written. A handler-argument
// decode error is reported without a close, and an Emit from OnError is delivered.
func TestUndecodablePacketReportedBeforeClose(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(WithEngineOptions(engineio.WithLogger(quiet)))
	errs, reasons := make(chan error, 4), make(chan string, 1)
	srv.OnConnect("/", func(Conn) error { return nil })
	srv.OnEvent("/", "num", func(_ Conn, n int) int { return n })
	srv.OnEvent("/", "ping", func(Conn) string { return "pong" })
	srv.OnError("/", func(c Conn, err error) {
		if !c.Connected() {
			err = fmt.Errorf("OnError ran after the close: %w", err)
		}
		c.Emit("reported", err.Error())
		errs <- err
	})
	srv.OnDisconnect("/", func(_ Conn, reason string) { reasons <- reason })
	go func() { _ = srv.Serve() }()
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { _ = srv.Close(); ts.Close() })

	ws, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/socket.io/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(waitFor)))
	read := func() (string, error) {
		_, b, err := ws.ReadMessage()
		return strings.TrimSpace(string(b)), err
	}
	next := func(prefix string) string {
		t.Helper()
		for {
			m, err := read()
			require.NoError(t, err)
			if strings.HasPrefix(m, prefix) {
				return m
			}
		}
	}
	next("40")

	// An argument that does not decode into the handler's parameter keeps the connection.
	require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte(`421["num","x"]`)))
	require.ErrorContains(t, recv(t, errs, "the decode error"), "decode argument 0")
	require.Equal(t, `42["reported","decode argument 0: json: cannot unmarshal string into Go value of type int"]`, next(`42["reported"`))
	require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte(`422["ping"]`)))
	require.Equal(t, `432["pong"]`, next("432"))

	// A packet whose payload is not an event array ends the connection after OnError.
	require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte(`42{"a":1}`)))
	require.EqualError(t, recv(t, errs, "the invalid packet"), "invalid payload")
	require.Equal(t, "forced close", recv(t, reasons, "OnDisconnect"))
	for { // the server closes the websocket
		if _, err := read(); err != nil {
			require.False(t, errors.Is(err, os.ErrDeadlineExceeded), "the websocket stayed open")
			break
		}
	}
}
