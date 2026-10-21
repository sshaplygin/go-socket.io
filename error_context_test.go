package socketio_test

import (
	"io"
	"net/http/httptest"
	"testing"
	"time"

	sio "github.com/sshaplygin/go-socket.io"
	ec "github.com/sshaplygin/go-socket.io/engineio/client"
	ep "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	ws "github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func TestErrorSocketRoomCleanup(t *testing.T) {
	s := sio.NewServer()
	defer func() { _ = s.Close() }()
	errs := make(chan sio.Conn, 1)
	s.OnError("/unused", func(c sio.Conn, err error) {
		if c != nil {
			c.Join("errors")
			errs <- c
		}
	})
	srv := httptest.NewServer(s)
	defer srv.Close()
	go func() { _ = s.Serve() }()
	c, err := (&ec.Dialer{Transports: []transport.Transport{ws.Default}}).Dial(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	_, r, err := c.NextReader()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, r); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	w, err := c.NextWriter(ep.String)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, `4/unused,"peer error"`); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case nc := <-errs:
		if nc.Connected() || len(nc.Rooms()) != 0 {
			t.Fatal("error context acquired live room membership")
		}
	case <-time.After(time.Second):
		t.Fatal("no OnError")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for s.Count() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.Count() != 0 {
		t.Fatal("connection did not close")
	}
	if n := s.RoomLen("/unused", "errors"); n != 0 {
		t.Fatalf("room retains %d socket after engine closed; registered namespace sockets=%d", n, len(s.Of("/unused").Sockets()))
	}
}
