package client

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

// TestDialSendsEIO4 checks that the dialer asks for protocol version 4, even when the
// URL names another one.
func TestDialSendsEIO4(t *testing.T) {
	var mu sync.Mutex
	var got []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.URL.Query().Get("EIO"))
		mu.Unlock()
		http.Error(w, "stop", http.StatusTeapot)
	}))
	defer ts.Close()

	d := &Dialer{Transports: []transport.Transport{polling.Default}}
	_, err := d.Dial(ts.URL+"/?EIO=3", nil)
	assert.Error(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"4"}, got)
}
