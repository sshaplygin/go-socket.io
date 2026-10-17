package engineio

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

type clientUpgradeFrames struct {
	values []string
	err    error
}

func clientUpgradeRead(conn Conn, firstTen func()) <-chan clientUpgradeFrames {
	result := make(chan clientUpgradeFrames, 1)
	go func() {
		var frames clientUpgradeFrames
		for {
			_, reader, err := conn.NextReader()
			if err != nil {
				frames.err = err
				break
			}
			data, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if readErr != nil || closeErr != nil {
				frames.err = fmt.Errorf("read %v; close %v", readErr, closeErr)
				break
			}
			if string(data) == "end" {
				break
			}
			frames.values = append(frames.values, string(data))
			if len(frames.values) == 10 && firstTen != nil {
				firstTen()
			}
		}
		result <- frames
	}()
	return result
}

func clientUpgradeWrite(conn Conn, switched func() bool) <-chan error {
	result := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		after := 0
		for i := 0; ; i++ {
			if time.Now().After(deadline) {
				result <- fmt.Errorf("transport did not upgrade: %s", conn.Transport())
				return
			}
			value := strconv.Itoa(i)
			if after == 10 {
				value = "end"
			}
			writer, err := conn.NextWriter(session.TEXT)
			if err == nil {
				_, err = io.WriteString(writer, value)
				closeErr := writer.Close()
				if err == nil {
					err = closeErr
				}
			}
			if err != nil || value == "end" {
				result <- err
				return
			}
			if switched() {
				after++
			}
		}
	}()
	return result
}

// Covers C5
func TestClientUpgradeOrder(t *testing.T) {
	s := NewServer(nil)
	firstTen := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(firstTen) }) }
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("transport") == "websocket" {
			select {
			case <-firstTen:
			case <-r.Context().Done():
				return
			}
		}
		s.ServeHTTP(w, r)
	}))
	var clientConn Conn
	t.Cleanup(func() {
		release()
		if clientConn != nil {
			_ = clientConn.Close()
		}
		_ = s.Close()
		ts.Close()
	})
	var err error
	clientConn, err = (&Dialer{Upgrade: true, Transports: []transport.Transport{polling.Default, websocket.Default}}).Dial(ts.URL, nil)
	require.NoError(t, err)
	serverConn, err := s.Accept()
	require.NoError(t, err)
	serverRead := clientUpgradeRead(serverConn, release)
	clientRead := clientUpgradeRead(clientConn, nil)
	serverWrite := clientUpgradeWrite(serverConn, func() bool { return serverConn.Transport() == "websocket" })
	clientWrite := clientUpgradeWrite(clientConn, clientConn.Upgraded)
	for _, written := range []<-chan error{clientWrite, serverWrite} {
		select {
		case err := <-written:
			require.NoError(t, err)
		case <-time.After(7 * time.Second):
			t.Fatal("upgrade writer did not finish")
		}
	}
	for side, read := range []<-chan clientUpgradeFrames{serverRead, clientRead} {
		select {
		case frames := <-read:
			require.NoError(t, frames.err)
			minimum := 10
			if side == 0 {
				minimum = 20 // the probe is held until ten client messages crossed polling
			}
			require.GreaterOrEqual(t, len(frames.values), minimum)
			for i, value := range frames.values {
				require.Equal(t, strconv.Itoa(i), value, "packet lost, repeated or reordered")
			}
		case <-time.After(7 * time.Second):
			t.Fatal("upgrade reader did not reach its end marker")
		}
	}
	require.True(t, clientConn.Upgraded())
	require.Equal(t, "websocket", clientConn.Transport())
	require.Equal(t, "websocket", serverConn.Transport())
}

func clientUpgradeMessage(conn Conn, value string) error {
	w, err := conn.NextWriter(session.TEXT)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(w, value)
	closeErr := w.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// Covers C5
func TestClientUpgradeProbeFails(t *testing.T) {
	for _, name := range []string{"rejected", "wrong-answer", "stalled", "unread", "clamped"} {
		t.Run(name, func(t *testing.T) {
			clientRecords, serverRecords := newRecorder(), newRecorder()
			setDefault(t, clientRecords)
			opts := &Options{Logger: slog.New(serverRecords)}
			if name == "clamped" {
				opts.PingInterval, opts.PingTimeout = 100*time.Millisecond, 100*time.Millisecond
			}
			s := NewServer(opts)
			ready, stop := make(chan struct{}), make(chan struct{})
			var readyOnce sync.Once
			release := func() { readyOnce.Do(func() { close(ready) }) }
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("transport") != "websocket" {
					s.ServeHTTP(w, r)
					return
				}
				select {
				case <-ready:
				case <-r.Context().Done():
					return
				}
				switch name {
				case "rejected":
					http.Error(w, "probe rejected", http.StatusBadRequest)
				case "wrong-answer":
					ws, err := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
					if err != nil {
						return
					}
					defer ws.Close()
					if _, _, err := ws.ReadMessage(); err == nil {
						_ = ws.WriteMessage(gorilla.TextMessage, []byte("3wrong"))
					}
				case "stalled", "clamped":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						return
					}
					defer conn.Close()
					<-stop
				default:
					s.ServeHTTP(w, r)
				}
			}))
			var cl Conn
			t.Cleanup(func() {
				release()
				close(stop)
				if cl != nil {
					_ = cl.Close()
				}
				_ = s.Close()
				ts.Close()
			})
			var bound time.Duration
			if name == "stalled" || name == "unread" {
				bound = 200 * time.Millisecond
			} else if name == "clamped" {
				bound = time.Hour
			}
			var err error
			cl, err = (&Dialer{Upgrade: true, UpgradeTimeout: bound,
				Transports: []transport.Transport{polling.Default, websocket.Default}}).Dial(ts.URL, nil)
			require.NoError(t, err)
			serverConn, err := s.Accept()
			require.NoError(t, err)
			serverRead := clientUpgradeRead(serverConn, nil)
			var clientRead <-chan clientUpgradeFrames
			if name == "unread" {
				require.NoError(t, clientUpgradeMessage(serverConn, "before"))
			} else {
				clientRead = clientUpgradeRead(cl, nil)
			}
			release()
			require.Eventually(t, func() bool {
				return len(clientRecords.find("engineio: upgrade probe failed")) != 0
			}, 1200*time.Millisecond, time.Millisecond)
			records := clientRecords.find("engineio: upgrade probe failed")
			require.Len(t, records, 1)
			require.Equal(t, "DEBUG", records[0]["level"])
			require.Equal(t, "websocket", records[0]["transport"])
			require.NotEmpty(t, records[0]["err"])
			if name == "wrong-answer" {
				require.Contains(t, records[0]["err"], "invalid upgrade probe answer")
			} else if name == "rejected" {
				require.Contains(t, records[0]["err"], "bad handshake")
			} else {
				require.Contains(t, records[0]["err"], "deadline exceeded")
			}
			require.NotContains(t, records[0], "sid")
			require.Equal(t, "polling", cl.Transport())
			require.False(t, cl.Upgraded())
			if name == "unread" {
				clientRead = clientUpgradeRead(cl, nil)
			}
			require.NoError(t, clientUpgradeMessage(cl, "client"))
			require.NoError(t, clientUpgradeMessage(cl, "end"))
			require.NoError(t, clientUpgradeMessage(serverConn, "server"))
			require.NoError(t, clientUpgradeMessage(serverConn, "end"))
			for side, result := range []<-chan clientUpgradeFrames{serverRead, clientRead} {
				select {
				case frames := <-result:
					require.NoError(t, frames.err)
					want := []string{"client"}
					if side == 1 {
						want = []string{"server"}
						if name == "unread" {
							want = []string{"before", "server"}
						}
					}
					require.Equal(t, want, frames.values)
				case <-time.After(2 * time.Second):
					t.Fatal("polling stopped working after a failed upgrade")
				}
			}
			closed := make(chan struct{})
			go func() { _ = cl.Close(); close(closed) }()
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("close waited for the stalled probe dial")
			}
		})
	}
}

// Covers C5
func TestClientUpgradeDeadline(t *testing.T) {
	answered, stop := make(chan struct{}), make(chan struct{})
	upgrade := make(chan error, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("transport") == "websocket" {
			ws, err := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
			if err != nil {
				upgrade <- err
				return
			}
			defer ws.Close()
			_, packet, err := ws.ReadMessage()
			if err != nil || string(packet) != "2probe" {
				upgrade <- fmt.Errorf("expected probe, got %q: %v", packet, err)
				return
			}
			if err := ws.WriteMessage(gorilla.TextMessage, []byte("3probe")); err != nil {
				upgrade <- err
				return
			}
			close(answered)
			_, packet, err = ws.ReadMessage()
			if err != nil || string(packet) != "5" {
				upgrade <- fmt.Errorf("expected UPGRADE, got %q: %v", packet, err)
				return
			}
			upgrade <- nil
			// Deliberately never send a PONG or another frame.
			for err == nil {
				_, _, err = ws.ReadMessage()
			}
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		if r.Method == http.MethodPost {
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = io.WriteString(w, "ok")
			return
		}
		if r.URL.Query().Get("sid") == "" {
			open := `0{"sid":"deadline","upgrades":["websocket"],"pingInterval":500,"pingTimeout":500}`
			_, _ = fmt.Fprintf(w, "%d:%s", len(open), open)
			return
		}
		select {
		case <-answered:
			_, _ = io.WriteString(w, "1:6")
		case <-stop:
		case <-r.Context().Done():
		}
	}))
	var cl Conn
	t.Cleanup(func() {
		close(stop)
		if cl != nil {
			_ = cl.Close()
		}
		ts.Close()
	})
	var err error
	cl, err = (&Dialer{Upgrade: true, Transports: []transport.Transport{polling.Default, websocket.Default}}).Dial(ts.URL+"?b64=1", nil)
	require.NoError(t, err)
	read := clientUpgradeRead(cl, nil)
	select {
	case err := <-upgrade:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("the peer did not receive UPGRADE")
	}
	require.Eventually(t, cl.Upgraded, time.Second, time.Millisecond)
	select {
	case result := <-read:
		var closed *session.CloseError
		require.True(t, errors.As(result.err, &closed), "%v", result.err)
		require.Equal(t, session.ReasonPingTimeout, closed.Reason)
	case <-time.After(2 * time.Second):
		t.Fatal("the upgraded reader has no heartbeat deadline")
	}
}
