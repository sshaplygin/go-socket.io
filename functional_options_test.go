package socketio_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
)

type optionSessionID struct{}

func (optionSessionID) NewID() string { return "configured-session" }

func TestServerOptionsDefaultOverrideAndReuse(t *testing.T) {
	cookie := &http.Cookie{Name: "configured", Path: "/"}
	shared := socketio.WithEngineOptions(
		engineio.WithPingInterval(33*time.Second),
		engineio.WithCookie(cookie),
		engineio.WithSessionIDGenerator(optionSessionID{}),
	)
	first := socketio.NewServer(shared, socketio.WithEngineOptions(
		engineio.WithPingInterval(44*time.Second),
		engineio.WithCookieEnabled(false),
		engineio.WithCORS(false),
		engineio.WithUpgrades(false),
	))
	second := socketio.NewServer(shared)
	defaults := socketio.NewServer()
	for _, server := range []*socketio.Server{first, second, defaults} {
		t.Cleanup(func() { require.NoError(t, server.Close()) })
	}
	for _, tc := range []struct {
		name     string
		server   *socketio.Server
		interval int
		cookie   string
		origin   bool
		upgrades []string
	}{
		{"override", first, 44000, "", false, []string{}},
		{"reused", second, 33000, "configured", true, []string{"websocket"}},
		{"default", defaults, 25000, "io", true, []string{"websocket"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/socket.io/?EIO=3&transport=polling&b64=1", nil)
			request.Header.Set("Origin", "https://consumer.example")
			response := httptest.NewRecorder()
			tc.server.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code)
			body := response.Body.String()
			start := strings.IndexByte(body, '{')
			require.GreaterOrEqual(t, start, 0)
			var open struct {
				SID          string   `json:"sid"`
				PingInterval int      `json:"pingInterval"`
				PingTimeout  int      `json:"pingTimeout"`
				Upgrades     []string `json:"upgrades"`
			}
			require.NoError(t, json.Unmarshal([]byte(body[start:]), &open))
			require.Equal(t, tc.interval, open.PingInterval)
			require.Equal(t, 20000, open.PingTimeout)
			require.Equal(t, tc.upgrades, open.Upgrades)
			if tc.server != defaults {
				require.Equal(t, "configured-session", open.SID)
			}
			if tc.cookie == "" {
				require.Empty(t, response.Result().Cookies())
			} else {
				cookies := response.Result().Cookies()
				require.Len(t, cookies, 1)
				require.Equal(t, tc.cookie, cookies[0].Name)
				require.Equal(t, open.SID, cookies[0].Value)
			}
			require.Equal(t, tc.origin, response.Header().Get("Access-Control-Allow-Origin") != "")
		})
	}
	require.Empty(t, cookie.Value, "constructors must not write a session ID into the caller's cookie")
}
