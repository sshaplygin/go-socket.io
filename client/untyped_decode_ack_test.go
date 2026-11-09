package client

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func untypedAckPeer(t *testing.T, ran chan<- string) *peer {
	t.Helper()
	return start(t, 'C', hooks{events: map[string]interface{}{
		"num":   func(Conn, int) string { ran <- "num"; return "num-ok" },
		"pair":  func(Conn, int, string) string { ran <- "pair"; return "pair-ok" },
		"rest":  func(Conn, string, ...int) string { ran <- "rest"; return "rest-ok" },
		"alive": func(Conn) string { return "alive" },
	}})
}

func clientInvalidPayloadACK(t *testing.T, message string) string {
	t.Helper()
	data, err := json.Marshal(message)
	require.NoError(t, err)
	return `[{"code":"invalid_payload","message":` + string(data) + "}]\n"
}

// TestClientUntypedHandlerDecodeFailureAnswersRequestedAck: when the server asked
// for an acknowledgement and the arguments do not decode into the handler's
// parameter types, the client answers with the error ACK of typed acknowledged
// handlers and reports the decode error to the error handler. Without an ACK id
// nothing is sent. The probe is sent right behind the packet under test, so a
// missing reply shows up as the probe's reply arriving first, not as a timeout.
func TestClientUntypedHandlerDecodeFailureAnswersRequestedAck(t *testing.T) {
	reported := func(t *testing.T, p *peer) {
		t.Helper()
		var typeErr *json.UnmarshalTypeError
		require.ErrorAs(t, recv(t, p.errs, "error handler").err, &typeErr)
	}
	for _, tc := range []struct {
		name, packet, want string
	}{
		{"wrong type", `24["num","bad"]`, "34" + clientInvalidPayloadACK(t, "decode argument 0: json: cannot unmarshal string into Go value of type int")},
		{"wrong type in a later argument", `25["pair",1,2]`, "35" + clientInvalidPayloadACK(t, "decode argument 1: json: cannot unmarshal number into Go value of type string")},
		{"wrong type in a variadic element", `26["rest","a",1,"x"]`, "36" + clientInvalidPayloadACK(t, "decode argument 2: json: cannot unmarshal string into Go value of type int")},
		{"object where a number is expected", `27["num",{"a":1}]`, "37" + clientInvalidPayloadACK(t, "decode argument 0: json: cannot unmarshal object into Go value of type int")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ran := make(chan string, 8)
			p := untypedAckPeer(t, ran)
			p.send(t, tc.packet)
			p.send(t, `29["alive"]`)
			require.Equal(t, tc.want, recv(t, p.fc.out, "error ACK"))
			require.Equal(t, "39[\"alive\"]\n", recv(t, p.fc.out, "probe ACK"))
			reported(t, p)
			require.Empty(t, ran, "the handler must not run")
			require.False(t, isDone(p.fc.closed))
		})
	}
	t.Run("wrong type in a namespace answers that namespace", func(t *testing.T) {
		ran := make(chan string, 8)
		p := start(t, 'C', hooks{events: map[string]interface{}{
			"num":   func(Conn, int) string { ran <- "num"; return "num-ok" },
			"alive": func(Conn) string { return "alive" },
		}}, "/chat")
		p.send(t, `2/chat,3["num","bad"]`)
		p.send(t, `29["alive"]`)
		require.Equal(t, "3/chat,3"+clientInvalidPayloadACK(t, "decode argument 0: json: cannot unmarshal string into Go value of type int"), recv(t, p.fc.out, "error ACK"))
		require.Equal(t, "39[\"alive\"]\n", recv(t, p.fc.out, "probe ACK"))
		require.Equal(t, "/chat", recv(t, p.errs, "error handler").nsp)
		require.Empty(t, ran)
	})
	t.Run("wrong type without ack sends nothing", func(t *testing.T) {
		ran := make(chan string, 8)
		p := untypedAckPeer(t, ran)
		p.send(t, `2["num","bad"]`)
		p.send(t, `29["alive"]`)
		require.Equal(t, "39[\"alive\"]\n", recv(t, p.fc.out, "probe ACK"))
		reported(t, p)
		require.Empty(t, ran)
		require.False(t, isDone(p.fc.closed))
	})
}

// TestClientUntypedHandlerLenientArgumentCountsStillRun: arguments that decode are
// handled as before. Missing arguments are zero-filled and extra ones are
// ignored, and the handler's return value is the ACK.
func TestClientUntypedHandlerLenientArgumentCountsStillRun(t *testing.T) {
	ran := make(chan string, 8)
	p := untypedAckPeer(t, ran)
	for _, tc := range []struct{ packet, handler, want string }{
		{`21["num"]`, "num", "31[\"num-ok\"]\n"},
		{`22["num",1,"extra",{"x":true}]`, "num", "32[\"num-ok\"]\n"},
		{`23["pair",7]`, "pair", "33[\"pair-ok\"]\n"},
		{`24["rest","a"]`, "rest", "34[\"rest-ok\"]\n"},
	} {
		p.send(t, tc.packet)
		require.Equal(t, tc.want, recv(t, p.fc.out, "ACK"))
		require.Equal(t, tc.handler, recv(t, ran, "handler"))
	}
	require.Empty(t, p.errs)
}
