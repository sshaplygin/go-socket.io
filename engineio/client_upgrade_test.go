package engineio

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

type upgradeFaultTransport struct {
	transport.Transport
	phase string
	err   error
}

func (t upgradeFaultTransport) Dial(u *url.URL, h http.Header) (transport.Conn, error) {
	c, err := t.Transport.Dial(u, h)
	if err != nil {
		return nil, err
	}
	return &upgradeFaultConn{Conn: c, phase: t.phase, err: t.err}, nil
}

type upgradeFaultConn struct {
	transport.Conn
	phase                         string
	err                           error
	readDeadlines, writeDeadlines int
}

func (c *upgradeFaultConn) SetReadDeadline(deadline time.Time) error {
	c.readDeadlines++
	if c.phase == "read-deadline" && c.readDeadlines == 1 || c.phase == "upgrade-read-deadline" && c.readDeadlines == 2 {
		return c.err
	}
	return c.Conn.SetReadDeadline(deadline)
}

func (c *upgradeFaultConn) SetWriteDeadline(deadline time.Time) error {
	c.writeDeadlines++
	if c.phase == "write-deadline" && c.writeDeadlines == 1 || c.phase == "upgrade-write-deadline" && c.writeDeadlines == 2 {
		return c.err
	}
	return c.Conn.SetWriteDeadline(deadline)
}

func (c *upgradeFaultConn) NextWriter(ft frame.Type, pt packet.Type) (io.WriteCloser, error) {
	if c.phase == "ping-next" && pt == packet.PING || c.phase == "upgrade-next" && pt == packet.UPGRADE {
		return nil, c.err
	}
	w, err := c.Conn.NextWriter(ft, pt)
	if err != nil {
		return nil, err
	}
	return &upgradeFaultWriter{WriteCloser: w, c: c, packet: pt}, nil
}

type upgradeFaultWriter struct {
	io.WriteCloser
	c      *upgradeFaultConn
	packet packet.Type
}

func (w *upgradeFaultWriter) Write(p []byte) (int, error) {
	if w.c.phase == "ping-write" {
		return 0, w.c.err
	}
	return w.WriteCloser.Write(p)
}

func (w *upgradeFaultWriter) Close() error {
	err := w.WriteCloser.Close()
	if w.c.phase == "ping-close" && w.packet == packet.PING || w.c.phase == "upgrade-close" && w.packet == packet.UPGRADE {
		return w.c.err
	}
	return err
}

func (c *upgradeFaultConn) NextReader() (frame.Type, packet.Type, io.ReadCloser, error) {
	if c.phase == "read-next" {
		return 0, 0, nil, c.err
	}
	ft, pt, r, err := c.Conn.NextReader()
	if err != nil {
		return ft, pt, r, err
	}
	return ft, pt, &upgradeFaultReader{ReadCloser: r, c: c}, nil
}

type upgradeFaultReader struct {
	io.ReadCloser
	c *upgradeFaultConn
}

func (r *upgradeFaultReader) Read(p []byte) (int, error) {
	if r.c.phase == "read-body" {
		return 0, r.c.err
	}
	return r.ReadCloser.Read(p)
}

func (r *upgradeFaultReader) Close() error {
	err := r.ReadCloser.Close()
	if r.c.phase == "read-close" {
		return r.c.err
	}
	return err
}

// Probe failures before UPGRADE preserve polling. A failure while committing it
// closes the connection because either endpoint may already have switched.
// Covers C5
func TestClientUpgradeTransportErrors(t *testing.T) {
	for _, phase := range []string{"read-deadline", "write-deadline", "ping-next", "ping-write", "ping-close", "read-next", "read-body", "read-close", "upgrade-next", "upgrade-close", "upgrade-read-deadline", "upgrade-write-deadline"} {
		t.Run(phase, func(t *testing.T) {
			records := newRecorder()
			setDefault(t, records)
			srv := NewServer(&Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			ts := httptest.NewServer(srv)
			var c Conn
			t.Cleanup(func() {
				if c != nil {
					_ = c.Close()
				}
				_ = srv.Close()
				ts.Close()
			})
			injected := errors.New("injected probe " + phase)
			candidate := upgradeFaultTransport{Transport: websocket.Default, phase: phase, err: injected}
			var err error
			c, err = (&Dialer{Upgrade: true, UpgradeTimeout: time.Second, Transports: []transport.Transport{polling.Default, candidate}}).Dial(ts.URL, nil)
			require.NoError(t, err)
			server, err := srv.Accept()
			require.NoError(t, err)
			clientRead, serverRead := clientUpgradeRead(c, nil), clientUpgradeRead(server, nil)
			require.Eventually(t, func() bool { return len(records.find("engineio: upgrade probe failed")) > 0 }, 2*time.Second, time.Millisecond)
			require.Contains(t, records.find("engineio: upgrade probe failed")[0]["err"], injected.Error())
			require.False(t, c.Upgraded())
			if strings.HasPrefix(phase, "upgrade-") {
				select {
				case result := <-clientRead:
					var closed *session.CloseError
					require.ErrorAs(t, result.err, &closed)
					require.Equal(t, session.ReasonTransportError, closed.Reason)
					require.ErrorIs(t, closed, injected)
				case <-time.After(time.Second):
					t.Fatal("failed UPGRADE did not close the client")
				}
				require.Equal(t, StateClosed, c.ReadyState())
			} else {
				require.Equal(t, "polling", c.Transport())
				require.NoError(t, clientUpgradeMessage(c, "client"))
				require.NoError(t, clientUpgradeMessage(c, "end"))
				require.NoError(t, clientUpgradeMessage(server, "server"))
				require.NoError(t, clientUpgradeMessage(server, "end"))
				for side, read := range []<-chan clientUpgradeFrames{clientRead, serverRead} {
					select {
					case result := <-read:
						require.NoError(t, result.err)
						want := "server"
						if side == 1 {
							want = "client"
						}
						require.Equal(t, []string{want}, result.values)
					case <-time.After(time.Second):
						t.Fatal("polling did not survive a probe failure")
					}
				}
			}
		})
	}
}

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
					defer func() { _ = ws.Close() }()
					if _, _, err := ws.ReadMessage(); err == nil {
						_ = ws.WriteMessage(gorilla.TextMessage, []byte("3wrong"))
					}
				case "stalled", "clamped":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						return
					}
					defer func() { _ = conn.Close() }()
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
			switch name {
			case "stalled", "unread":
				bound = 200 * time.Millisecond
			case "clamped":
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
			switch name {
			case "wrong-answer":
				require.Contains(t, records[0]["err"], "invalid upgrade probe answer")
			case "rejected":
				require.Contains(t, records[0]["err"], "bad handshake")
			default:
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
			defer func() { _ = ws.Close() }()
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
