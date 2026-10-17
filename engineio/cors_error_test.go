package engineio

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/stretchr/testify/require"
)

func TestCORSErrorResponses(t *testing.T) {
	for _, policy := range []string{"default", "disabled", "enabled allow", "enabled deny", "disabled allow", "disabled deny"} {
		t.Run(policy, func(t *testing.T) {
			disabled := policy == "disabled" || policy == "disabled allow" || policy == "disabled deny"
			allowed := !disabled
			opts := &Options{DisableCORS: disabled, RequestChecker: func(r *http.Request) (http.Header, error) {
				if r.URL.Query().Has("deny") {
					return nil, errors.New("denied")
				}
				return nil, nil
			}}
			if policy != "default" && policy != "disabled" {
				allowed = policy == "enabled allow" || policy == "disabled allow"
				check := func(*http.Request) bool { return allowed }
				opts.Transports = []transport.Transport{&polling.Transport{CheckOrigin: check}, &websocket.Transport{CheckOrigin: check}}
			}
			s := NewServer(opts)
			ts := httptest.NewServer(s)
			client := &http.Client{Timeout: time.Second}
			t.Cleanup(func() { _ = s.Close(); ts.Close(); client.CloseIdleConnections() })
			for _, origin := range []string{"", foreignOrigin} {
				for _, rejection := range []struct {
					method, query string
					status, code  int
				}{
					{http.MethodGet, "transport=polling&sid=missing", 400, 1},
					{http.MethodOptions, "transport=polling", 400, 2},
					{http.MethodGet, "transport=polling&deny", 403, 4},
					{http.MethodGet, "transport=websocket&sid=missing", 400, 1},
					{http.MethodGet, "transport=unknown", 400, 0},
				} {
					response, body := corsRequest(t, client, rejection.method, ts.URL+"/?EIO=3&"+rejection.query, origin, "")
					require.Equal(t, rejection.status, response.StatusCode)
					require.Contains(t, body, `"code":`+strconv.Itoa(rejection.code))
					wantOrigin, credentials := "", ""
					wantAllowed := allowed
					if rejection.code == 0 {
						wantAllowed = !disabled
					}
					if wantAllowed {
						wantOrigin = origin
						if origin == "" {
							wantOrigin = "*"
						} else {
							credentials = "true"
						}
					}
					require.Equal(t, wantOrigin, response.Header.Get("Access-Control-Allow-Origin"), policy+" "+rejection.query)
					require.Equal(t, credentials, response.Header.Get("Access-Control-Allow-Credentials"), policy+" "+rejection.query)
				}
			}
		})
	}
}
