package websocket

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// rawPeer connects a raw gorilla connection to a transport connection in the
// given role: the server accepts, or the client dials, with query.
func rawPeer(t *testing.T, server bool, query string) (transport.Conn, *gorilla.Conn) {
	t.Helper()
	tr := &Transport{}
	accepted := make(chan transport.Conn, 1)
	raw := make(chan *gorilla.Conn, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if server {
			c, err := tr.Accept(w, r)
			if err != nil {
				accepted <- nil
				return
			}
			accepted <- c
			c.(http.Handler).ServeHTTP(w, r)
			return
		}
		c, err := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			raw <- nil
			return
		}
		raw <- c
	}))
	t.Cleanup(ts.Close)
	u, err := url.Parse("ws" + strings.TrimPrefix(ts.URL, "http") + "/?EIO=3" + query)
	require.NoError(t, err)
	var conn transport.Conn
	var peer *gorilla.Conn
	if server {
		peer, _, err = gorilla.DefaultDialer.Dial(u.String(), nil)
		require.NoError(t, err)
		conn = <-accepted
	} else {
		conn, err = tr.Dial(u, nil)
		require.NoError(t, err)
		peer = <-raw
	}
	require.NotNil(t, conn)
	require.NotNil(t, peer)
	t.Cleanup(func() { _ = peer.Close(); _ = conn.Close() })
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	return conn, peer
}

// Both roles read a "b<type><base64>" text message as a binary packet with or
// without b64, and write binary packets as base64 text only with b64.
func TestWebsocketBase64Packets(t *testing.T) {
	for _, server := range []bool{true, false} {
		for _, query := range []string{"", "&b64=1"} {
			name := "client"
			if server {
				name = "server"
			}
			t.Run(name+query, func(t *testing.T) {
				conn, peer := rawPeer(t, server, query)
				for _, in := range []struct {
					typ  int
					data string
					ft   eioparser.FrameType
					pt   eioparser.PacketType
					want string
				}{
					{gorilla.TextMessage, "b4AQI=", eioparser.Binary, eioparser.MESSAGE, "\x01\x02"},
					{gorilla.TextMessage, "b4", eioparser.Binary, eioparser.MESSAGE, ""},
					{gorilla.TextMessage, "b2cHJvYmU=", eioparser.Binary, eioparser.PING, "probe"},
					{gorilla.TextMessage, "4b4AQI=", eioparser.String, eioparser.MESSAGE, "b4AQI="},
					{gorilla.TextMessage, "6", eioparser.String, eioparser.NOOP, ""},
					{gorilla.BinaryMessage, "\x04b4AQI=", eioparser.Binary, eioparser.MESSAGE, "b4AQI="},
				} {
					require.NoError(t, peer.WriteMessage(in.typ, []byte(in.data)))
					ft, pt, r, err := conn.NextReader()
					require.NoError(t, err, "%q", in.data)
					data, err := io.ReadAll(r)
					require.NoError(t, err, "%q", in.data)
					require.NoError(t, r.Close(), "%q", in.data)
					require.Equal(t, in.ft, ft, "%q", in.data)
					require.Equal(t, in.pt, pt, "%q", in.data)
					require.Equal(t, in.want, string(data), "%q", in.data)
				}

				w, err := conn.NextWriter(eioparser.Binary, eioparser.MESSAGE)
				require.NoError(t, err)
				_, err = w.Write([]byte{1, 2})
				require.NoError(t, err)
				require.NoError(t, w.Close())
				typ, data, err := peer.ReadMessage()
				require.NoError(t, err)
				if query == "" {
					require.Equal(t, gorilla.BinaryMessage, typ)
					require.Equal(t, []byte{4, 1, 2}, data)
				} else {
					require.Equal(t, gorilla.TextMessage, typ)
					require.Equal(t, "b4AQI=", string(data))
				}
			})
		}
	}
}

// A malformed base64 packet fails the read in both roles and modes.
func TestWebsocketBase64PacketsMalformed(t *testing.T) {
	for _, server := range []bool{true, false} {
		for _, query := range []string{"", "&b64=1"} {
			for _, packet := range []string{"b", "b4%%%", "b4AQ", "b4AQI=AQ==", "b4AQI\x00"} {
				conn, peer := rawPeer(t, server, query)
				require.NoError(t, peer.WriteMessage(gorilla.TextMessage, []byte(packet)))
				_, _, r, err := conn.NextReader()
				if err == nil {
					_, err = io.ReadAll(r)
					err = errors.Join(err, r.Close())
				}
				require.Error(t, err, "server=%v query=%q packet=%q", server, query, packet)
			}
		}
	}
}

// messageFrames returns one message and then io.EOF.
type messageFrames struct {
	ft   eioparser.FrameType
	data []byte
	done bool
}

func (m *messageFrames) NextReader() (eioparser.FrameType, io.ReadCloser, error) {
	if m.done {
		return 0, nil, io.EOF
	}
	m.done = true
	return m.ft, io.NopCloser(bytes.NewReader(m.data)), nil
}

// FuzzPacketReader checks the packet decoding of one websocket message against
// the definition: a text message "b<type><data>" carries base64.StdEncoding
// data and fails when that does not decode; any other message passes its bytes
// after the type through.
func FuzzPacketReader(f *testing.F) {
	for _, seed := range []string{"", "4", "4hello", "b", "b4", "b4AQI=", "b4AQI", "b4AQ==", "b4AQI=AQ==", "b4%%%", "b4AQ\r\nI=", "b2cHJvYmU=", "\x04\x01\x02"} {
		f.Add(seed, false)
		f.Add(seed, true)
	}
	f.Fuzz(func(t *testing.T, message string, binary bool) {
		ft := eioparser.String
		if binary {
			ft = eioparser.Binary
		}
		gotFT, pt, r, err := packetReader{&messageFrames{ft: ft, data: []byte(message)}}.NextReader()
		minLen := 1
		base64Packet := !binary && strings.HasPrefix(message, "b")
		if base64Packet {
			minLen = 2
		}
		if len(message) < minLen {
			if err == nil {
				t.Fatalf("message %q of %d bytes decoded", message, len(message))
			}
			return
		}
		if err != nil {
			t.Fatalf("message %q: %v", message, err)
		}
		data, readErr := io.ReadAll(r)
		readErr = errors.Join(readErr, r.Close())
		if !base64Packet {
			if gotFT != ft || pt != eioparser.ByteToPacketType(message[0], ft) || readErr != nil || string(data) != message[1:] {
				t.Fatalf("message %q: got %v %v %q %v", message, gotFT, pt, data, readErr)
			}
			return
		}
		if gotFT != eioparser.Binary || pt != eioparser.ByteToPacketType(message[1], eioparser.String) {
			t.Fatalf("message %q: got frame %v packet %v", message, gotFT, pt)
		}
		want, wantErr := base64.StdEncoding.DecodeString(message[2:])
		if (wantErr != nil) != (readErr != nil) {
			t.Fatalf("message %q: decode error %v, want %v", message, readErr, wantErr)
		}
		if wantErr == nil && !bytes.Equal(data, want) {
			t.Fatalf("message %q: got %v, want %v", message, data, want)
		}
	})
}
