package socketio

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// untypedAckServer registers handlers whose argument types the test payloads
// either match or violate. "alive" is the probe: its reply is the next packet
// the peer observes once the packet under test has been handled.
func untypedAckServer(t *testing.T, ran chan<- string) (url string, errs <-chan error) {
	t.Helper()
	reports := make(chan error, 8)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnError("/", func(_ Conn, err error) { reports <- err })
		s.OnEvent("/", "num", func(_ Conn, n int) string { ran <- "num"; return "num-ok" })
		s.OnEvent("/", "pair", func(_ Conn, n int, s string) string { ran <- "pair"; return "pair-ok" })
		s.OnEvent("/", "rest", func(_ Conn, head string, tail ...int) string { ran <- "rest"; return "rest-ok" })
		s.OnEvent("/", "alive", func(Conn) string { return "alive" })
		s.OnConnect("/chat", func(Conn) error { return nil })
		s.OnError("/chat", func(_ Conn, err error) { reports <- err })
		s.OnEvent("/chat", "num", func(_ Conn, n int) string { ran <- "chat-num"; return "chat-ok" })
	})
	return ts.URL, reports
}

func sendAttachment(t *testing.T, c *rawClient, data string) {
	t.Helper()
	w, err := c.conn.NextWriter(eioparser.Binary)
	require.NoError(t, err)
	_, err = io.WriteString(w, data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

func invalidPayloadACK(t *testing.T, message string) string {
	t.Helper()
	data, err := json.Marshal(message)
	require.NoError(t, err)
	return `[{"code":"invalid_payload","message":` + string(data) + "}]\n"
}

// TestUntypedHandlerDecodeFailureAnswersRequestedAck: when the peer asked for an
// acknowledgement and the arguments do not decode into the handler's parameter
// types, the server answers with the error ACK of typed acknowledged handlers and
// reports the decode error to OnError. Without an ACK id nothing is sent.
func TestUntypedHandlerDecodeFailureAnswersRequestedAck(t *testing.T) {
	ran := make(chan string, 8)
	var errs <-chan error
	// Each case has its own server and connection, so one failure does not shift
	// the replies or reports of the cases after it.
	connect := func(t *testing.T) *rawClient {
		t.Helper()
		var url string
		url, errs = untypedAckServer(t, ran)
		c := dialRaw(t, url)
		require.Equal(t, "0", c.read())
		return c
	}

	// The probe is sent right behind the packet under test, so a missing reply
	// shows up as the probe's ACK arriving first, not as a wait for a timeout.
	// Replies are in order, and the probe's reply must be the last packet.
	sendProbe := func(c *rawClient) { c.send(`29["alive"]`) }
	probeReply := func(t *testing.T, c *rawClient) {
		t.Helper()
		require.Equal(t, "39[\"alive\"]\n", c.read(), "the probe reply must follow the replies under test")
	}
	reported := func(t *testing.T) {
		t.Helper()
		var typeErr *json.UnmarshalTypeError
		require.ErrorAs(t, recv(t, errs, "OnError"), &typeErr)
	}

	t.Run("wrong type with ack", func(t *testing.T) {
		c := connect(t)
		c.send(`24["num","bad"]`)
		sendProbe(c)
		require.Equal(t, "34"+invalidPayloadACK(t, "decode argument 0: json: cannot unmarshal string into Go value of type int"), c.read())
		probeReply(t, c)
		reported(t)
	})
	t.Run("wrong type in a later argument with ack", func(t *testing.T) {
		c := connect(t)
		c.send(`25["pair",1,2]`)
		sendProbe(c)
		require.Equal(t, "35"+invalidPayloadACK(t, "decode argument 1: json: cannot unmarshal number into Go value of type string"), c.read())
		probeReply(t, c)
		reported(t)
	})
	t.Run("wrong type in a variadic element with ack", func(t *testing.T) {
		c := connect(t)
		c.send(`26["rest","a",1,"x"]`)
		sendProbe(c)
		require.Equal(t, "36"+invalidPayloadACK(t, "decode argument 2: json: cannot unmarshal string into Go value of type int"), c.read())
		probeReply(t, c)
		reported(t)
	})
	t.Run("object where a number is expected with ack", func(t *testing.T) {
		c := connect(t)
		c.send(`27["num",{"a":1}]`)
		sendProbe(c)
		require.Equal(t, "37"+invalidPayloadACK(t, "decode argument 0: json: cannot unmarshal object into Go value of type int"), c.read())
		probeReply(t, c)
		reported(t)
	})
	t.Run("binary attachment where a number is expected with ack", func(t *testing.T) {
		c := connect(t)
		c.send(`51-8["num",{"_placeholder":true,"num":0}]`)
		sendAttachment(t, c, "bytes")
		sendProbe(c) // read as a header only if the attachment was consumed
		require.Contains(t, c.read(), `"code":"invalid_payload"`)
		probeReply(t, c)
		reported(t)
	})
	t.Run("wrong type in a namespace answers that namespace", func(t *testing.T) {
		c := connect(t)
		c.send("0/chat,")
		require.Equal(t, "0/chat,", c.read())
		c.send(`2/chat,3["num","bad"]`)
		sendProbe(c)
		require.Equal(t, "3/chat,3"+invalidPayloadACK(t, "decode argument 0: json: cannot unmarshal string into Go value of type int"), c.read())
		probeReply(t, c)
		reported(t)
	})
	t.Run("wrong type without ack sends nothing", func(t *testing.T) {
		c := connect(t)
		c.send(`2["num","bad"]`)
		c.send(`51-["num",{"_placeholder":true,"num":0}]`)
		sendAttachment(t, c, "bytes")
		sendProbe(c)
		probeReply(t, c)
		reported(t)
		reported(t)
	})

	select {
	case name := <-ran:
		t.Fatalf("handler %s ran for arguments that do not decode", name)
	default:
	}
}

// TestUntypedHandlerLenientArgumentCountsStillRun: arguments that decode are
// handled as before. Missing arguments are zero-filled and extra ones are
// ignored, and the handler's return value is the ACK.
func TestUntypedHandlerLenientArgumentCountsStillRun(t *testing.T) {
	ran := make(chan string, 8)
	url, errs := untypedAckServer(t, ran)
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())

	c.send(`21["num"]`)
	require.Equal(t, "31[\"num-ok\"]\n", c.read(), "a missing argument is zero-filled")
	require.Equal(t, "num", recv(t, ran, "handler"))
	c.send(`22["num",1,"extra",{"x":true}]`)
	require.Equal(t, "32[\"num-ok\"]\n", c.read(), "extra arguments are ignored")
	require.Equal(t, "num", recv(t, ran, "handler"))
	c.send(`23["pair",7]`)
	require.Equal(t, "33[\"pair-ok\"]\n", c.read())
	require.Equal(t, "pair", recv(t, ran, "handler"))
	c.send(`24["rest","a"]`)
	require.Equal(t, "34[\"rest-ok\"]\n", c.read())
	require.Equal(t, "rest", recv(t, ran, "handler"))
	requireNoReport(t, errs)
}

// TestUntypedHandlerNonArrayPayloadClosesConnection pins the existing behavior
// for an EVENT whose payload is not an array: the packet is invalid, no ACK is
// sent and the connection is closed. This is a framing error, not an argument
// decode failure.
func TestUntypedHandlerNonArrayPayloadClosesConnection(t *testing.T) {
	ran := make(chan string, 8)
	url, _ := untypedAckServer(t, ran)
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send(`25"num"`)
	require.True(t, c.readClosed())
	select {
	case name := <-ran:
		t.Fatalf("handler %s ran", name)
	default:
	}
}
