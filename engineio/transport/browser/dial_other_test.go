//go:build !js

package browser_test

import (
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/transport/browser"
)

func TestDialIsUnsupported(t *testing.T) {
	u := &url.URL{Scheme: "https", Host: "example.com", Path: "/socket.io/", RawQuery: "EIO=3"}
	c, err := (&browser.Transport{}).Dial(u, nil)
	require.True(t, errors.Is(err, errors.ErrUnsupported), "%v", err)
	require.Nil(t, c)
	require.Equal(t, "wss", u.Scheme, "Dial maps the URL as the websocket transport does")
	require.Equal(t, "websocket", u.Query().Get("transport"))
	require.NotEmpty(t, u.Query().Get("t"))
}
