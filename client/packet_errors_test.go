package client

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

func TestClientErrorPackets(t *testing.T) {
	for _, tc := range []struct{ name, packet, want string }{
		{"string", `4"refused"`, "refused"},
		{"object", `4{"reason":"refused"}`, `{"reason":"refused"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := start(t, 'C', hooks{events: map[string]interface{}{"echo": func(Conn) string { return "ok" }}})
			p.send(t, tc.packet)
			require.EqualError(t, recv(t, p.errs, "ERROR callback").err, tc.want)
			p.send(t, `27["echo"]`)
			require.Equal(t, "37[\"ok\"]\n", recv(t, p.fc.out, "ACK after ERROR"))
			require.False(t, isDone(p.fc.closed))
		})
	}
	t.Run("malformed", func(t *testing.T) {
		p := start(t, 'C', hooks{})
		p.send(t, `4{`)
		require.Error(t, recv(t, p.errs, "malformed ERROR report").err)
		recv(t, p.fc.closed, "close after malformed ERROR")
		p.disconnected(t, "/")
	})
}

func TestClientExplicitAckOwnership(t *testing.T) {
	acks := make(chan AckFunc, 1)
	p := start(t, 'C', hooks{events: map[string]interface{}{
		"late": func(_ Conn, value string, ack AckFunc) string { acks <- ack; return "ignored" },
	}})
	p.send(t, `27["late","question"]`)
	ack := recv(t, acks, "deferred ACK callback")
	expectBeforeBarrier(t, p) // the handler's return value produced no automatic ACK
	ack("answer")
	ack("duplicate")
	expectBeforeBarrier(t, p, "37[\"answer\"]\n")
	p.send(t, `2["late","no acknowledgement requested"]`)
	recv(t, acks, "callback without an ACK request")("ignored")
	expectBeforeBarrier(t, p)
}

func TestClientAckErrors(t *testing.T) {
	t.Run("type error and stale IDs keep the connection", func(t *testing.T) {
		p := start(t, 'C', hooks{events: map[string]interface{}{"echo": func(Conn) string { return "ok" }}})
		p.nc.Emit("ask", func(int) { t.Error("invalid ACK reached callback") })
		require.Equal(t, "21[\"ask\"]\n", recv(t, p.fc.out, "outbound question"))
		p.send(t, `31["wrong type"]`)
		require.Error(t, recv(t, p.errs, "ACK decode error").err)
		p.send(t, `31[]`)
		p.send(t, `399[]`)
		p.send(t, `3/missing,1[]`)
		p.send(t, `27["echo"]`)
		require.Equal(t, "37[\"ok\"]\n", recv(t, p.fc.out, "ACK after ignored stale IDs"))
		require.False(t, isDone(p.fc.closed))
	})
	for _, kind := range []string{"panic", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			p := start(t, 'C', hooks{})
			p.nc.Emit("ask", func() { panic("ACK callback failed") })
			require.Equal(t, "21[\"ask\"]\n", recv(t, p.fc.out, "outbound question"))
			packet := `31[]`
			if kind == "malformed" {
				packet = `31{`
			}
			p.send(t, packet)
			err := recv(t, p.errs, "ACK error report").err
			require.Error(t, err)
			if kind == "panic" {
				require.ErrorContains(t, err, "ACK callback failed")
			}
			recv(t, p.fc.closed, "close after ACK error")
			p.disconnected(t, "/")
		})
	}
}

func TestClientRegistrationAndDialFailures(t *testing.T) {
	for _, register := range []struct {
		name  string
		apply func(*Client)
	}{
		{"disconnect", func(c *Client) { c.OnDisconnect(func(Conn, string) {}) }},
		{"error", func(c *Client) { c.OnError(func(Conn, error) {}) }},
		{"event", func(c *Client) { c.OnEvent("event", func(Conn) {}) }},
	} {
		t.Run(register.name, func(t *testing.T) {
			c, err := NewClient("http://127.0.0.1/", nil)
			require.NoError(t, err)
			register.apply(c)
			failure := errors.New("dial failed")
			c.dial = func(string) (engineio.Conn, error) { return nil, failure }
			require.ErrorIs(t, c.Connect(), failure)
		})
	}
	t.Run("missing root handler uses the configured logger", func(t *testing.T) {
		var records bytes.Buffer
		log := slog.New(slog.NewTextHandler(&records, nil))
		c, err := NewClient("http://127.0.0.1/", &engineio.Options{Logger: log})
		require.NoError(t, err)
		fc := newFakeConn(t)
		c.dial = func(string) (engineio.Conn, error) { return fc, nil }
		require.ErrorIs(t, c.Connect(), errUnavailableRootHandler)
		recv(t, fc.closed, "engine close after connect rejection")
		require.Contains(t, records.String(), "socketio: unhandled error")
		require.Contains(t, records.String(), errUnavailableRootHandler.Error())
	})
}
