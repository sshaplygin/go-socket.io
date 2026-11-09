package engineio_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

var sidPattern = regexp.MustCompile(`"sid":"([^"]+)"`)

// handshake performs the polling handshake and returns the session ID from the
// OPEN packet. With a held-open server the call returns while the server-side
// Init has not returned yet.
func handshake(t *testing.T, base string) string {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(base + "/?EIO=3&transport=polling&b64=1")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	m := sidPattern.FindSubmatch(body)
	require.NotNil(t, m, "no sid in handshake response %q", body)
	return string(m[1])
}

// Once a client has the OPEN packet it can ask the server for its sessions and
// must find itself, in agreement with Count.
func TestClientsListsSessionOnceClientHasOpenPacket(t *testing.T) {
	s, release := newHeldOpenServer(t)
	ts := httptest.NewServer(s)
	defer ts.Close()
	defer func() { _ = s.Close() }()
	defer release()

	sid := handshake(t, ts.URL)

	clients := s.Clients()
	require.Equal(t, 1, s.Count())
	require.Len(t, clients, 1)
	require.Contains(t, clients, sid)
	require.Equal(t, engineio.StateOpen, clients[sid].ReadyState())
}

// Closing the session while Init is still returning must not leave a registered
// session or hand it to Accept.
func TestCloseAfterOpenDeliveredBeforeInitReturns(t *testing.T) {
	s, release := newHeldOpenServer(t)
	ts := httptest.NewServer(s)
	defer ts.Close()
	defer func() { _ = s.Close() }()
	defer release()

	sid := handshake(t, ts.URL)
	conn, ok := s.Clients()[sid]
	require.True(t, ok, "session is not listed after the client received OPEN")
	require.NoError(t, conn.Close())
	release()

	require.Eventually(t, func() bool { return s.Count() == 0 }, 5*time.Second, time.Millisecond,
		"closed session stays registered")
	require.Empty(t, s.Clients())
	accepted := make(chan struct{})
	go func() {
		if _, err := s.Accept(); err == nil {
			close(accepted)
		}
	}()
	select {
	case <-accepted:
		t.Fatal("a session closed during Init was accepted")
	case <-time.After(100 * time.Millisecond):
	}
}

// Server.Close during the window ends the session and unregisters it.
func TestServerCloseAfterOpenDeliveredBeforeInitReturns(t *testing.T) {
	s, release := newHeldOpenServer(t)
	ts := httptest.NewServer(s)
	defer ts.Close()
	defer release()

	sid := handshake(t, ts.URL)
	require.Contains(t, s.Clients(), sid)
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	release()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Server.Close did not return")
	}
	require.Eventually(t, func() bool { return s.Count() == 0 }, 5*time.Second, time.Millisecond)
	require.Empty(t, s.Clients())
}
