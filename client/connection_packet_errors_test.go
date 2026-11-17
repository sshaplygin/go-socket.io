package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/internal/event"
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
		apply func(*manager)
	}{
		{"disconnect", func(c *manager) { c.OnDisconnect(func(Conn, string) {}) }},
		{"error", func(c *manager) { c.OnError(func(Conn, error) {}) }},
		{"event", func(c *manager) { c.OnEvent("event", func(Conn) {}) }},
	} {
		t.Run(register.name, func(t *testing.T) {
			c, err := newTestManager(t, "http://127.0.0.1/")
			require.NoError(t, err)
			register.apply(c)
			failure := errors.New("dial failed")
			c.dial = func(string) (engineclient.Conn, error) { return nil, failure }
			require.ErrorIs(t, c.Connect(), failure)
		})
	}
	t.Run("default root handler uses the configured logger", func(t *testing.T) {
		var records bytes.Buffer
		log := slog.New(slog.NewTextHandler(&records, nil))
		c, err := newTestManager(t, "http://127.0.0.1/", WithLogger(log))
		require.NoError(t, err)
		fc := newFakeConn(t)
		c.dial = func(string) (engineclient.Conn, error) { return fc, nil }
		require.NoError(t, c.Connect())
		closeAtEnd(t, fc, c.state.conn)
		fc.reads <- `4"refused"`
		// A following handled event is the wire barrier for the ERROR dispatch.
		c.OnEvent("barrier", func(Conn) string { return "ok" })
		fc.reads <- "0"
		fc.reads <- `27["barrier"]`
		require.Equal(t, "37[\"ok\"]\n", recv(t, fc.out, "ERROR dispatch barrier"))
		require.Contains(t, records.String(), "socketio: unhandled error")
		require.Contains(t, records.String(), "refused")
	})
}

// Without a client OnError, a typed handler panic is logged once with its stack.
func TestTypedClientHandlerPanicWithoutOnErrorIsLogged(t *testing.T) {
	var records bytes.Buffer
	c, err := newTestManager(t, "http://127.0.0.1/", WithLogger(slog.New(slog.NewJSONHandler(&records, nil))))
	require.NoError(t, err)
	fc := newFakeConn(t)
	c.dial = func(string) (engineclient.Conn, error) { return fc, nil }
	require.NoError(t, c.RegisterEvent("fail", event.NewHandler(event.SchemaOf[int](), true, func(context.Context, event.Endpoint, event.Payload) (event.Payload, error) {
		panic("typed failure")
	})))
	c.OnEvent("barrier", func(Conn) string { return "ok" })
	require.NoError(t, c.Connect())
	closeAtEnd(t, fc, c.state.conn)
	fc.reads <- "0"
	fc.reads <- `23["fail",1]`
	require.Equal(t, "33[{\"code\":\"handler_panic\",\"message\":\"internal error\"}]\n", recv(t, fc.out, "panic acknowledgement"))
	// The barrier is dispatched after the panic was reported.
	fc.reads <- `27["barrier"]`
	require.Equal(t, "37[\"ok\"]\n", recv(t, fc.out, "barrier"))
	var panics []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(records.String()), "\n") {
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record), line)
		if strings.Contains(line, "typed failure") {
			panics = append(panics, record)
		}
	}
	require.Len(t, panics, 1, records.String())
	require.Equal(t, "socketio: handler panic", panics[0]["msg"])
	require.Equal(t, "WARN", panics[0]["level"])
	require.Equal(t, "panic: typed failure", panics[0]["err"])
	require.Contains(t, panics[0]["stack"], "connection_packet_errors_test.go")
}
