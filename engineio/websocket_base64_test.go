package engineio

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dialWebsocketSession opens a session over a direct websocket connection with
// the given extra query and returns the server side and the raw client.
func dialWebsocketSession(t *testing.T, query string, opts ...Option) (Conn, *websocket.Conn) {
	t.Helper()
	server := NewServer(opts...)
	ts := httptest.NewServer(server)
	t.Cleanup(func() { _ = server.Close(); ts.Close() })
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket"+query, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, _, err = ws.ReadMessage()
	require.NoError(t, err)
	conn, err := server.Accept()
	require.NoError(t, err)
	return conn, ws
}

// upgradeWebsocketSession opens a polling session and upgrades it to a
// websocket connection with the given extra query.
func upgradeWebsocketSession(t *testing.T, query string) (Conn, *websocket.Conn) {
	t.Helper()
	f := newParityWire(t, nil)
	target := "ws" + strings.TrimPrefix(f.http.URL, "http") + "/?EIO=3&transport=websocket" + query + "&sid=" + f.params.SID
	ws, _, err := websocket.DefaultDialer.Dial(target, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte("2probe")))
	_, packet, err := ws.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "3probe", string(packet))
	code, body := f.request(t, http.MethodGet, f.poll, "")
	require.Equal(t, 200, code)
	require.Equal(t, "1:6", body)
	require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte("5")))
	require.Eventually(t, f.conn.Upgraded, time.Second, time.Millisecond)
	return f.conn, ws
}

func readPacket(t *testing.T, conn Conn) (eioparser.FrameType, []byte) {
	t.Helper()
	ft, reader, err := conn.NextReader()
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	return ft, data
}

// A websocket text message "b<type><base64>" is a binary packet whether or not
// the request has b64, as in engine.io 3.6.2: socket.io-client before 1.4 under
// Node sends attachments that way without asking for b64. The server writes
// binary packets as base64 text only when the request has b64.
func TestWebsocketBase64Frames(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		for _, b64 := range []bool{false, true} {
			name := "direct"
			if upgrade {
				name = "upgrade"
			}
			query := ""
			if b64 {
				name += "/b64"
				query = "&b64=1"
			}
			t.Run(name, func(t *testing.T) {
				var conn Conn
				var ws *websocket.Conn
				if upgrade {
					conn, ws = upgradeWebsocketSession(t, query)
				} else {
					conn, ws = dialWebsocketSession(t, query)
				}

				writer, err := conn.NextWriter(eioparser.Binary)
				require.NoError(t, err)
				_, err = writer.Write([]byte{1, 2})
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				typ, packet, err := ws.ReadMessage()
				require.NoError(t, err)
				// Check the inbound direction even when the outbound frame is wrong.
				if b64 {
					assert.Equal(t, websocket.TextMessage, typ, "outbound binary packet frame type")
					assert.Equal(t, "b4AQI=", string(packet), "outbound binary packet encoding")
				} else {
					assert.Equal(t, websocket.BinaryMessage, typ, "outbound binary packet frame type")
					assert.Equal(t, []byte{4, 1, 2}, packet, "outbound binary packet encoding")
				}

				for _, in := range []struct {
					typ  int
					data string
					want []byte
				}{
					{websocket.TextMessage, "b4AQI=", []byte{1, 2}},
					{websocket.TextMessage, "b4", []byte{}},
					{websocket.BinaryMessage, "\x04\x01\x02", []byte{1, 2}},
				} {
					require.NoError(t, ws.WriteMessage(in.typ, []byte(in.data)))
					ft, data := readPacket(t, conn)
					require.Equal(t, eioparser.Binary, ft, "%q", in.data)
					require.Equal(t, in.want, data, "%q", in.data)
				}
				// A text packet that is not base64 keeps its type.
				require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte("4b4AQI=")))
				ft, data := readPacket(t, conn)
				require.Equal(t, eioparser.String, ft)
				require.Equal(t, "b4AQI=", string(data))
				require.Equal(t, StateOpen, conn.ReadyState())
			})
		}
	}
}

func TestWebsocketBase64Malformed(t *testing.T) {
	for _, query := range []string{"", "&b64=1"} {
		for _, packet := range []string{"", "b", "b4%%%", "b4AQ", "b4AQI=AQ==", "b9AQI="} {
			t.Run(query+"/"+packet, func(t *testing.T) {
				conn, ws := dialWebsocketSession(t, query)
				require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(packet)))
				_, r, err := conn.NextReader()
				if err == nil {
					_, err = io.ReadAll(r)
					_ = r.Close()
				}
				require.Error(t, err, "malformed base64 packet should fail decoding")
			})
		}
	}
}

// MaxHTTPBufferSize applies to the websocket message as sent, the base64 text,
// as the maxPayload of engine.io 3.6.2 does.
func TestWebsocketBase64MessageLimit(t *testing.T) {
	conn, ws := dialWebsocketSession(t, "", WithMaxHTTPBufferSize(8))
	// Six bytes on the wire carry three bytes of data.
	require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte("b4AQID")))
	ft, data := readPacket(t, conn)
	require.Equal(t, eioparser.Binary, ft)
	require.Equal(t, []byte{1, 2, 3}, data)
	// Ten bytes on the wire carry four bytes of data, under the limit decoded.
	require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte("b4AQIDBA==")))
	_, _, err := conn.NextReader()
	require.Error(t, err)
	_, _, err = ws.ReadMessage()
	require.True(t, websocket.IsCloseError(err, websocket.CloseMessageTooBig), "%v", err)
}
