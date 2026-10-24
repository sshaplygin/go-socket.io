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

func TestWebsocketBase64Mode(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "direct"
		if upgrade {
			name = "upgrade"
		}
		t.Run(name, func(t *testing.T) {
			var conn Conn
			var ws *websocket.Conn
			if upgrade {
				f := newParityWire(t, nil)
				target := "ws" + strings.TrimPrefix(f.http.URL, "http") + "/?EIO=3&transport=websocket&b64=1&sid=" + f.params.SID
				var err error
				ws, _, err = websocket.DefaultDialer.Dial(target, nil)
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
				conn = f.conn
				require.Eventually(t, conn.Upgraded, time.Second, time.Millisecond)
			} else {
				server := NewServer()
				ts := httptest.NewServer(server)
				t.Cleanup(func() { _ = server.Close(); ts.Close() })
				var err error
				ws, _, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket&b64=1", nil)
				require.NoError(t, err)
				t.Cleanup(func() { _ = ws.Close() })
				require.NoError(t, ws.SetReadDeadline(time.Now().Add(2*time.Second)))
				_, _, err = ws.ReadMessage()
				require.NoError(t, err)
				conn, err = server.Accept()
				require.NoError(t, err)
			}
			writer, err := conn.NextWriter(eioparser.Binary)
			require.NoError(t, err)
			_, err = writer.Write([]byte{1, 2})
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			typ, packet, err := ws.ReadMessage()
			require.NoError(t, err)
			// Check the inbound direction even when the outbound frame is wrong.
			assert.Equal(t, websocket.TextMessage, typ, "outbound binary packet frame type")
			assert.Equal(t, "b4AQI=", string(packet), "outbound binary packet encoding")
			require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte("b4AQI=")))
			ft, reader, err := conn.NextReader()
			require.NoError(t, err)
			require.Equal(t, eioparser.Binary, ft)
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.Equal(t, []byte{1, 2}, data)
			require.NoError(t, reader.Close())
			require.Equal(t, StateOpen, conn.ReadyState())
		})
	}
}

func TestWebsocketBase64Malformed(t *testing.T) {
	for _, packet := range []string{"", "b", "b4%%%", "b4AQ", "b9AQI="} {
		t.Run(packet, func(t *testing.T) {
			server := NewServer()
			ts := httptest.NewServer(server)
			t.Cleanup(func() { _ = server.Close(); ts.Close() })
			ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket&b64=1", nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = ws.Close() })
			require.NoError(t, ws.SetReadDeadline(time.Now().Add(time.Second)))
			_, _, err = ws.ReadMessage()
			require.NoError(t, err)
			conn, err := server.Accept()
			require.NoError(t, err)
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
