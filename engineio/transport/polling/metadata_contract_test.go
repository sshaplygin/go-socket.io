package polling

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAcceptedPollingURLPreservesRequestTarget(t *testing.T) {
	request := httptest.NewRequest("GET", "https://socket.example/custom%2Fengine/?EIO=3&transport=polling&token=a%2Bb", nil)
	c, err := Default.Accept(httptest.NewRecorder(), request)
	require.NoError(t, err)
	defer func() { require.NoError(t, c.Close()) }()
	got := c.URL()
	require.Equal(t, request.URL.String(), got.String())
	require.Equal(t, "/custom/engine/", got.Path)
	require.Equal(t, "/custom%2Fengine/", got.EscapedPath())
	require.Equal(t, "a+b", got.Query().Get("token"))
	got.Path, got.RawQuery = "/other", "token=changed"
	again := c.URL()
	require.Equal(t, request.URL.String(), again.String(), "a returned URL value must not overwrite connection metadata")
}
