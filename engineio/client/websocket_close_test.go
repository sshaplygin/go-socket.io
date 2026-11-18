package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func receive[T any](t *testing.T, ch <-chan T, what string) (v T) {
	t.Helper()
	select {
	case v = <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for the %s", what)
	}
	return v
}

// timerOnly dials probes that ignore deadlines, so only the probe timer ends an unanswered probe.
type timerOnly struct{ *websocket.Transport }
type timerOnlyConn struct{ transport.Conn }

func (t timerOnly) Dial(u *url.URL, h http.Header) (transport.Conn, error) {
	c, err := t.Transport.Dial(u, h)
	if err != nil {
		return nil, err
	}
	return timerOnlyConn{c}, nil
}
func (timerOnlyConn) SetReadDeadline(time.Time) error  { return nil }
func (timerOnlyConn) SetWriteDeadline(time.Time) error { return nil }
func (c timerOnlyConn) Abort() error                   { return c.Conn.(interface{ Abort() error }).Abort() }

// Close after the client's own ping timeout sends a normal closure frame, as an
// orderly Close does (TestGoClientCloseIsTransportClose). After a transport error,
// a failed handshake and for a probe not switched to, there is no frame (1006).
func TestClientWebsocketCloseFrame(t *testing.T) {
	open, normal, dropped := `0{"sid":"s","upgrades":["websocket"],"pingInterval":25000,"pingTimeout":5000}`, gorilla.CloseNormalClosure, gorilla.CloseAbnormalClosure
	for _, tc := range []struct {
		name, probeReply string
		send             []string      // the messages of the session; an empty one has no packet type
		dialFails        bool          // Dial returns an error; nothing is closed after it
		reason           CloseReason   // read before Close when set
		upgrade          time.Duration // UpgradeTimeout; zero does not probe
		probeAtClose     bool          // the probe is still open when Close runs
		session, probe   int           // expected close codes; zero: no probe
	}{
		{name: "ping timeout", send: []string{`0{"sid":"s","upgrades":[],"pingInterval":50,"pingTimeout":50}`}, reason: ReasonPingTimeout, session: normal},
		{name: "transport error", send: []string{open, ""}, reason: ReasonTransportError, session: dropped},
		{name: "failed handshake", send: []string{"4x"}, dialFails: true, session: dropped},
		{name: "failed probe", send: []string{open}, upgrade: 10 * time.Second, probeReply: "3wrong", session: normal, probe: dropped},
		{name: "probe at close", send: []string{open}, upgrade: 10 * time.Second, probeAtClose: true, session: normal, probe: dropped},
		{name: "expired probe", send: []string{open}, upgrade: 300 * time.Millisecond, session: normal, probe: dropped},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessions, probes, probing := make(chan int, 1), make(chan int, 1), make(chan struct{}, 1) // close codes; the probe's 2probe
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ws, err := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer func() { _ = ws.Close() }()
				codes := probes
				if r.URL.Query().Get("sid") == "" {
					codes = sessions
					for _, m := range tc.send {
						_ = ws.WriteMessage(gorilla.TextMessage, []byte(m))
					}
				}
				for err == nil {
					var data []byte
					if _, data, err = ws.ReadMessage(); string(data) == "2probe" {
						probing <- struct{}{}
						if tc.probeReply != "" {
							_ = ws.WriteMessage(gorilla.TextMessage, []byte(tc.probeReply))
						}
					}
				}
				code, closed := dropped, (*gorilla.CloseError)(nil) // dropped: no close frame, or a reset
				if errors.As(err, &closed) {
					code = closed.Code
				}
				codes <- code
			}))
			defer ts.Close()
			dialer := &Dialer{Transports: []transport.Transport{websocket.Default}, Upgrade: tc.upgrade > 0, UpgradeTimeout: tc.upgrade}
			if dialer.Upgrade {
				dialer.Transports = append(dialer.Transports, timerOnly{websocket.Default})
			}
			cl, err := dialer.Dial(ts.URL, nil)
			if require.Equal(t, tc.dialFails, err != nil, "%v", err); !tc.dialFails {
				if tc.reason != "" {
					var closed *CloseError
					require.ErrorAs(t, readAll(cl), &closed)
					require.Equal(t, tc.reason, closed.Reason)
				}
				if tc.probeAtClose {
					receive(t, probing, "probe")
				} else if tc.probe != 0 {
					require.Equal(t, tc.probe, receive(t, probes, "probe close"))
				}
				_ = cl.Close()
			}
			require.Equal(t, tc.session, receive(t, sessions, "session close"))
			if tc.probeAtClose {
				require.Equal(t, tc.probe, receive(t, probes, "probe close"))
			}
		})
	}
}
