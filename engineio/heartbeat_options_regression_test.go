package engineio_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/stretchr/testify/require"
)

// Covers: G22
func TestReviewG22NegativeHeartbeatNotAdvertised(t *testing.T) {
	for i, opts := range [][]engineio.Option{
		{engineio.WithPingTimeout(-time.Second), engineio.WithPingInterval(time.Minute)},
		{engineio.WithPingTimeout(time.Minute), engineio.WithPingInterval(-time.Second)},
	} {
		t.Run([]string{"timeout", "interval"}[i], func(t *testing.T) {
			s := engineio.NewServer(opts...)
			ts := httptest.NewServer(s)
			defer ts.Close()
			defer s.Close()
			resp, err := http.Get(ts.URL + "/?EIO=3&transport=polling&b64=1")
			require.NoError(t, err)
			defer resp.Body.Close()
			b, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			if resp.StatusCode == http.StatusOK {
				require.NotContains(t, string(b), `"pingTimeout":-`)
				require.NotContains(t, string(b), `"pingInterval":-`)
			}
		})
	}
}

