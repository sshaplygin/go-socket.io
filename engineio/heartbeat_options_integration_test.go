package engineio_test

import (
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNegativeHeartbeatNotAdvertised(t *testing.T) {
	for i, opts := range [][]engineio.Option{
		{engineio.WithPingTimeout(-time.Second), engineio.WithPingInterval(time.Minute)},
		{engineio.WithPingTimeout(time.Minute), engineio.WithPingInterval(-time.Second)},
	} {
		t.Run([]string{"timeout", "interval"}[i], func(t *testing.T) {
			s := engineio.NewServer(opts...)
			ts := httptest.NewServer(s)
			defer ts.Close()
			defer func() { _ = s.Close() }()
			resp, err := http.Get(ts.URL + "/?EIO=3&transport=polling&b64=1")
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			b, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.NotContains(t, string(b), `"pingTimeout":-`)
			require.NotContains(t, string(b), `"pingInterval":-`)
			if i == 0 {
				require.Contains(t, string(b), `"pingTimeout":20000`)
			} else {
				require.Contains(t, string(b), `"pingInterval":25000`)
			}
		})
	}
}
