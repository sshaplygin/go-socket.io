package polling

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultHTTPTransportPreservesCustomRoundTripper(t *testing.T) {
	if runDefaultHTTPTransportInSubprocess(t) {
		return
	}
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
	if runDefaultHTTPTransportInSubprocess(t) {
		return
	}
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

// runDefaultHTTPTransportInSubprocess isolates global transport overrides from
// asynchronous requests started by other tests. Serial tests alone cannot stop
// those requests from reading http.DefaultTransport after their test returns.
func runDefaultHTTPTransportInSubprocess(t *testing.T) bool {
	t.Helper()
	const childEnv = "SOCKETIO_DEFAULT_TRANSPORT_CHILD"
	if os.Getenv(childEnv) == t.Name() {
		return false
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable,
		"-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.count=1", "-test.timeout=30s")
	command.Env = append(os.Environ(), childEnv+"="+t.Name())
	output, err := command.CombinedOutput()
	require.NoError(t, err, "isolated default transport test: %s", output)
	return true
}
