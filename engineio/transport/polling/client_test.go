package polling

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/payload"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// pollServer answers the open request with params, keeps every poll open until
// the client aborts it and records the POST bodies.
type pollServer struct {
	*httptest.Server
	mu       sync.Mutex
	posts    []string
	aborted  chan struct{}
	pollOnce sync.Once
}

func newPollServer(t *testing.T, params transport.ConnParameters) *pollServer {
	s := &pollServer{aborted: make(chan struct{})}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		switch {
		case r.URL.Query().Get("sid") == "":
			var open strings.Builder
			_, _ = params.WriteTo(&open)
			_, _ = io.WriteString(w, "0"+open.String())
		case r.Method == http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			s.mu.Lock()
			s.posts = append(s.posts, string(b))
			s.mu.Unlock()
			_, _ = io.WriteString(w, "ok")
		default:
			<-r.Context().Done()
			s.pollOnce.Do(func() { close(s.aborted) })
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func dialPollServer(t *testing.T, s *pollServer, tr *Transport) (transport.Conn, transport.ConnParameters) {
	u, err := url.Parse(s.URL)
	require.NoError(t, err)
	conn, err := tr.Dial(u, nil)
	require.NoError(t, err)
	params, err := conn.(*clientConn).Open()
	require.NoError(t, err)
	return conn, params
}

func TestClientCloseAbortsPoll(t *testing.T) {
	s := newPollServer(t, transport.ConnParameters{SID: "sid", PingInterval: time.Second, PingTimeout: time.Second})
	conn, _ := dialPollServer(t, s, &Transport{})

	// Wait for the long poll to start, then close: the server sees the abort.
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, conn.Close())
	select {
	case <-s.aborted:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not abort the pending poll")
	}

	// The abort is the close, not a failure: readers see io.EOF.
	_, _, _, err := conn.NextReader()
	assert.Equal(t, io.EOF, err)
}

// TestClientPostBatchesWithinMaxPayload sends packets that together exceed the
// maxPayload the server advertised: no POST body is larger than it, none is lost.
func TestClientPostBatchesWithinMaxPayload(t *testing.T) {
	const maxPayload = 12 // "4aaaa" is 5 bytes, two with a separator are 11
	s := newPollServer(t, transport.ConnParameters{
		SID: "sid", PingInterval: time.Second, PingTimeout: time.Second, MaxPayload: maxPayload,
	})
	conn, params := dialPollServer(t, s, &Transport{})
	defer func() { _ = conn.Close() }()
	require.Equal(t, maxPayload, params.MaxPayload)

	const writers = 6
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, err := conn.NextWriter(frame.String, packet.MESSAGE)
			if !assert.NoError(t, err) {
				return
			}
			_, _ = w.Write([]byte("aaaa"))
			assert.NoError(t, w.Close())
		}()
	}
	wg.Wait()

	// Close returns once the body is in the request buffer, so wait for the POSTs.
	count := func() (total int) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, body := range s.posts {
			total += len(strings.Split(body, "\x1e"))
		}
		return total
	}
	require.Eventually(t, func() bool { return count() == writers }, 5*time.Second, time.Millisecond)

	s.mu.Lock()
	defer s.mu.Unlock()
	t.Logf("POST bodies: %q", s.posts)
	for _, body := range s.posts {
		assert.LessOrEqual(t, len(body), maxPayload, fmt.Sprintf("%q", body))
	}
}

func TestClientRejectsOversizedPacket(t *testing.T) {
	s := newPollServer(t, transport.ConnParameters{SID: "sid", PingInterval: time.Second, PingTimeout: time.Second, MaxPayload: 8})
	conn, _ := dialPollServer(t, s, &Transport{})
	defer func() { _ = conn.Close() }()

	w, err := conn.NextWriter(frame.String, packet.MESSAGE)
	require.NoError(t, err)
	_, _ = w.Write([]byte("0123456789"))
	assert.ErrorContains(t, w.Close(), "too large")
}

// TestClientPostDefaultLimitWithoutAdvertisedMaxPayload: with a default Transport
// and an open packet that carries no maxPayload, concurrent writers whose
// combined size is over payload.DefaultMaxPayload are split across POSTs.
func TestClientPostDefaultLimitWithoutAdvertisedMaxPayload(t *testing.T) {
	s := newPollServer(t, transport.ConnParameters{SID: "sid", PingInterval: time.Second, PingTimeout: time.Second})
	conn, params := dialPollServer(t, s, &Transport{})
	defer func() { _ = conn.Close() }()
	require.Zero(t, params.MaxPayload)

	const writers, size = 3, 600 << 10
	data := []byte(strings.Repeat("a", size))
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, err := conn.NextWriter(frame.String, packet.MESSAGE)
			if !assert.NoError(t, err) {
				return
			}
			_, _ = w.Write(data)
			assert.NoError(t, w.Close())
		}()
	}
	wg.Wait()

	count := func() (total int) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, body := range s.posts {
			total += len(strings.Split(body, "\x1e"))
		}
		return total
	}
	require.Eventually(t, func() bool { return count() == writers }, 5*time.Second, time.Millisecond)

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, body := range s.posts {
		assert.LessOrEqual(t, len(body), payload.DefaultMaxPayload)
	}
}
