package engineio

import (
	"fmt"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/stretchr/testify/require"
)

func TestConnInitorBeforeAccept(t *testing.T) {
	for _, tr := range []string{"polling", "websocket"} {
		t.Run(tr, func(t *testing.T) {
			type contextRecord struct {
				URL string
				Err error
			}
			s := NewServer(&Options{ConnInitor: func(r *http.Request, c Conn) {
				c.SetContext(contextRecord{r.URL.String(), r.Context().Err()})
			}})
			srv := httptest.NewServer(s)
			defer srv.Close()
			defer func() { _ = s.Close() }()
			checked := make(chan error, 1)
			go func() {
				for i := 0; i < 50; i++ {
					c, err := s.Accept()
					if err != nil {
						checked <- err
						return
					}
					record, ok := c.Context().(contextRecord)
					if !ok || record.Err != nil || !strings.Contains(record.URL, "EIO=3") {
						checked <- fmt.Errorf("initor context not visible: %#v", c.Context())
						return
					}
					_ = c.Close()
					s.Remove(c.ID())
				}
				checked <- nil
			}()
			for i := 0; i < 50; i++ {
				u := srv.URL + "/?EIO=3&transport=" + tr
				if tr == "websocket" {
					c, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(u, "http"), nil)
					require.NoError(t, err)
					_, b, err := c.ReadMessage()
					require.NoError(t, err)
					require.True(t, strings.HasPrefix(string(b), "0"))
					_ = c.Close()
				} else {
					resp, err := http.Get(u)
					require.NoError(t, err)
					_, err = io.Copy(io.Discard, resp.Body)
					require.NoError(t, err)
					require.NoError(t, resp.Body.Close())
					require.Equal(t, 200, resp.StatusCode)
				}
			}
			select {
			case err := <-checked:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("Accept did not observe initialised sessions")
			}
		})
	}
	t.Run("rejected", func(t *testing.T) {
		for _, tr := range []string{"polling", "websocket"} {
			t.Run(tr, func(t *testing.T) {
				s := NewServer(&Options{ConnInitor: func(_ *http.Request, c Conn) { _ = c.Close() }})
				srv := httptest.NewServer(s)
				defer srv.Close()
				defer func() { _ = s.Close() }()
				accepted := make(chan Conn, 50)
				done := make(chan struct{})
				go func() {
					defer close(done)
					for {
						c, err := s.Accept()
						if err != nil {
							return
						}
						accepted <- c
					}
				}()
				for i := 0; i < 20; i++ {
					u := srv.URL + "/?EIO=3&transport=" + tr
					if tr == "websocket" {
						c, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(u, "http"), nil)
						require.NoError(t, err)
						require.NoError(t, c.SetReadDeadline(time.Now().Add(time.Second)))
						_, _, err = c.ReadMessage()
						require.Error(t, err)
						_ = c.Close()
					} else {
						resp, err := http.Get(u)
						require.NoError(t, err)
						require.NoError(t, resp.Body.Close())
					}
				}
				require.Eventually(t, func() bool { return s.Count() == 0 }, time.Second, time.Millisecond)
				require.NoError(t, s.Close())
				<-done
				require.Empty(t, accepted, "a rejected handshake reached Accept")
			})
		}
	})
}

// Covers K5
func TestSessionRequest(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		c := newBlockingConn()
		s, err := session.New(c, "request", "polling", eioparser.ConnParameters{PingInterval: time.Second, PingTimeout: time.Second}, nil)
		require.NoError(t, err)
		defer func() { _ = s.Close() }()
		require.Nil(t, s.Request())
		r := httptest.NewRequest("GET", "/handshake?token=ok", nil)
		s.SetRequest(r)
		require.Same(t, r, s.Request())
	})
	for _, tr := range []string{"polling", "websocket"} {
		t.Run(tr, func(t *testing.T) {
			s := NewServer(nil)
			srv := httptest.NewServer(s)
			defer srv.Close()
			defer func() { _ = s.Close() }()
			impl := map[string]transport.Transport{"polling": polling.Default, "websocket": websocket.Default}[tr]
			d := Dialer{Transports: []transport.Transport{impl}}
			client, err := d.Dial(srv.URL+"/?token=ok", http.Header{"X-Request": []string{"retained"}})
			require.NoError(t, err)
			defer func() { _ = client.Close() }()
			c, err := s.Accept()
			require.NoError(t, err)
			r := c.(*session.Session).Request()
			require.NotNil(t, r)
			require.Equal(t, "GET", r.Method)
			require.Equal(t, "ok", r.URL.Query().Get("token"))
			require.Equal(t, "retained", r.Header.Get("X-Request"))
			require.Equal(t, tr, r.URL.Query().Get("transport"))
		})
	}
	t.Run("upgrade", func(t *testing.T) {
		f := newParityWire(t, nil)
		r := f.conn.(*session.Session).Request()
		c := f.probe(t)
		reads := parityReader(f.conn)
		require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("5")))
		require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("4ready")))
		parityMessage(t, reads, "ready")
		require.Same(t, r, f.conn.(*session.Session).Request())
		require.Equal(t, "polling", r.URL.Query().Get("transport"))
	})
}
