package polling

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultHTTPTransportPreservesCustomRoundTripper(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	var called bool
	http.DefaultTransport = dialOpenRoundTripper(func(req *http.Request) (*http.Response, error) {
		called = true
		require.Equal(t, "http://polling.test/custom", req.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("custom transport")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	client := &http.Client{Transport: defaultHTTPTransport()}
	resp, err := client.Get("http://polling.test/custom")
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, "custom transport", string(body))
}

func TestDefaultHTTPTransportClonesStandardTransport(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	base := &http.Transport{MaxIdleConnsPerHost: 7, ResponseHeaderTimeout: 3 * time.Second}
	http.DefaultTransport = base
	cloned := defaultHTTPTransport()
	require.IsType(t, base, cloned)
	tuned := cloned.(*http.Transport)
	require.NotSame(t, base, tuned)
	require.Equal(t, 64, tuned.MaxIdleConnsPerHost)
	require.Equal(t, base.ResponseHeaderTimeout, tuned.ResponseHeaderTimeout)
	require.Equal(t, 7, base.MaxIdleConnsPerHost)
	require.Same(t, base, http.DefaultTransport)
}
