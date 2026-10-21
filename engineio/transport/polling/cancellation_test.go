package polling_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	ep "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/stretchr/testify/require"
)

func TestCloseCancelsHTTPRequests(t *testing.T) {
	for _, phase := range []string{"open", "get", "post"} {
		t.Run(phase, func(t *testing.T) {
			entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				opening := r.URL.Query().Get("sid") == ""
				target := phase == "open" && opening || phase == "get" && !opening && r.Method == http.MethodGet || phase == "post" && r.Method == http.MethodPost
				if target {
					if r.Method == http.MethodPost {
						_, _ = io.Copy(io.Discard, r.Body)
					}
					close(entered)
					select {
					case <-r.Context().Done():
						close(canceled)
					case <-release:
					}
					return
				}
				if opening {
					payload := `0{"sid":"test","upgrades":[],"pingInterval":10000,"pingTimeout":10000}`
					w.Header().Set("Content-Type", "text/plain;charset=UTF-8")
					_, _ = fmt.Fprintf(w, "%d:%s", len(payload), payload)
					return
				}
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer h.Close()
			defer close(release)
			u, err := url.Parse(h.URL + "?b64=1")
			require.NoError(t, err)
			// A custom HTTP client without a timeout must still be cancellable by Close.
			c, err := (&polling.Transport{Client: &http.Client{}}).Dial(u, nil)
			require.NoError(t, err)
			defer func() { _ = c.Close() }()
			opened := make(chan error, 1)
			go func() {
				_, err := c.(interface {
					Open() (ep.ConnParameters, error)
				}).Open()
				opened <- err
			}()
			if phase != "open" {
				select {
				case err := <-opened:
					require.NoError(t, err)
				case <-time.After(time.Second):
					t.Fatal("OPEN did not complete")
				}
			}
			if phase == "post" {
				w, err := c.NextWriter(ep.String, ep.MESSAGE)
				require.NoError(t, err)
				_, err = w.Write([]byte("event"))
				require.NoError(t, err)
				require.NoError(t, w.Close())
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("request did not reach server")
			}
			require.NoError(t, c.Close())
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("Close did not cancel HTTP request")
			}
			if phase == "open" {
				select {
				case err := <-opened:
					require.Error(t, err)
				case <-time.After(time.Second):
					t.Fatal("Open did not stop")
				}
			}
		})
	}
}
