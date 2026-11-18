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
	for _, tc := range []struct{ scheme, want string }{
		{scheme: "http", want: "ws"},
		{scheme: "https", want: "wss"},
		{scheme: "wss", want: "wss"},
	} {
		t.Run(tc.scheme, func(t *testing.T) {
			u := &url.URL{Scheme: tc.scheme, Host: "example.com", Path: "/socket.io/", RawQuery: "EIO=3"}
			c, err := (&browser.Transport{}).Dial(u, nil)
			require.True(t, errors.Is(err, errors.ErrUnsupported), "%v", err)
			require.Nil(t, c)
			require.Equal(t, tc.want, u.Scheme, "Dial maps the URL as the websocket transport does")
			require.Equal(t, "3", u.Query().Get("EIO"))
			require.Equal(t, "websocket", u.Query().Get("transport"))
			require.NotEmpty(t, u.Query().Get("t"))
		})
	}
}
