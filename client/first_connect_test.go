package client_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
)

// A failed first Connect returns its error and starts no reconnection, although
// reconnection is enabled.
func TestFailedFirstConnectIsNotRetried(t *testing.T) {
	requests := make(chan struct{}, 64)
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case requests <- struct{}{}:
		default:
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(refusing.Close)
	cl, err := client.New(refusing.URL, retryOptions()...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	attempts := make(chan int, 16)
	cl.OnReconnectAttempt(func(n int) { attempts <- n })
	require.Error(t, cl.Connect())
	recv(t, requests, "handshake request of the first Connect")
	// The reconnection delay is 10 ms; a longer wait only strengthens the check.
	select {
	case n := <-attempts:
		t.Fatalf("reconnect attempt %d after a failed first Connect", n)
	case <-requests:
		t.Fatal("another handshake request after a failed first Connect")
	case <-time.After(500 * time.Millisecond):
	}
}
