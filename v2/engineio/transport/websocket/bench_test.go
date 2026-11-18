package websocket

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
)

// BenchmarkWriteMessage measures one Engine.IO packet written as a WebSocket
// message through transport.Conn (NextWriter, Write, Close) on a loopback TCP
// connection, from the server side (unmasked) and the client side (masked), for a
// small text packet and a 64 KiB binary packet. The connection comes from a
// zero Transport's Accept and Dial, so the transport's defaults apply. The peer
// discards the bytes from the socket without decoding them.
func BenchmarkWriteMessage(b *testing.B) {
	sizes := []struct {
		name string
		ft   frame.Type
		data []byte
	}{
		{"small", frame.String, []byte("hello, engine.io")},
		{"64KiB", frame.Binary, bytes.Repeat([]byte{0xa5}, 64<<10)},
	}
	for _, side := range []string{"server", "client"} {
		for _, size := range sizes {
			b.Run(side+"/"+size.name, func(b *testing.B) {
				client, server := benchConnPair(b, &Transport{})
				writer, peer := server, client
				if side == "client" {
					writer, peer = client, server
				}
				drained := make(chan struct{})
				go func() {
					defer close(drained)
					_, _ = io.Copy(io.Discard, peer.(*conn).mc.raw)
				}()
				b.Cleanup(func() {
					_ = writer.Close()
					_ = peer.Close()
					<-drained
				})
				b.SetBytes(int64(len(size.data)))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					w, err := writer.NextWriter(size.ft, packet.MESSAGE)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := w.Write(size.data); err != nil {
						b.Fatal(err)
					}
					if err := w.Close(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// benchConnPair dials a server running tr and returns the client and server ends.
func benchConnPair(b *testing.B, tr *Transport) (client, server transport.Conn) {
	b.Helper()
	accepted := make(chan transport.Conn, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := tr.Accept(w, r)
		if err != nil {
			b.Errorf("Accept: %v", err)
			close(accepted)
			return
		}
		accepted <- c
	}))
	defer s.Close()
	u, err := url.Parse(s.URL)
	if err != nil {
		b.Fatal(err)
	}
	u.Scheme = "ws"
	client, err = tr.Dial(u, nil)
	if err != nil {
		b.Fatal(err)
	}
	server, ok := <-accepted
	if !ok {
		_ = client.Close()
		b.FailNow()
	}
	return client, server
}
