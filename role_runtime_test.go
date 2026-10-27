package socketio_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// rolePeer runs identical observable scenarios against each independent role.
// The other endpoint is a raw wire peer, so it cannot hide a runtime difference.
type rolePeer struct {
	wire         *gorilla.Conn
	on           func(string, func(string), bool) func()
	off          func(string)
	failures     chan error
	disconnected chan struct{}
	close        func()
}

func receiveRole[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for role runtime")
		var zero T
		return zero
	}
}

func newRolePeer(t *testing.T, role string) *rolePeer {
	t.Helper()
	p := &rolePeer{failures: make(chan error, 8), disconnected: make(chan struct{}, 1)}
	ready := make(chan struct{}, 1)
	var once sync.Once
	if role == "server" {
		s := socketio.NewServer()
		n := s.Of("/")
		n.OnConnect(func(socketio.Conn) error { ready <- struct{}{}; return nil })
		n.OnError(func(_ socketio.Conn, err error) { p.failures <- err })
		n.OnDisconnect(func(socketio.Conn, string) { p.disconnected <- struct{}{} })
		p.on = func(name string, f func(string), one bool) func() {
			handler := func(_ socketio.Conn, value string) { f(value) }
			if one {
				return n.Once(name, handler)
			}
			return n.On(name, handler)
		}
		p.off = n.Off
		served := make(chan error, 1)
		go func() { served <- s.Serve() }()
		ts := httptest.NewServer(s)
		p.close = func() {
			once.Do(func() {
				if p.wire != nil {
					_ = p.wire.Close()
				}
				_ = s.Close()
				ts.Close()
				require.NoError(t, receiveRole(t, served))
			})
		}
		t.Cleanup(p.close)
		var err error
		p.wire, _, err = gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
		require.NoError(t, err)
		require.NoError(t, p.wire.SetReadDeadline(time.Now().Add(3*time.Second)))
		_, open, err := p.wire.ReadMessage()
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(string(open), "0{"))
		_, connected, err := p.wire.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, "40", string(connected))
	} else {
		wires := make(chan *gorilla.Conn, 1)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wire, err := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			if err := wire.WriteMessage(gorilla.TextMessage, []byte(`0{"sid":"paired","upgrades":[],"pingInterval":60000,"pingTimeout":60000}`)); err != nil {
				t.Error(err)
				_ = wire.Close()
				return
			}
			if err := wire.WriteMessage(gorilla.TextMessage, []byte("40")); err != nil {
				t.Error(err)
				_ = wire.Close()
				return
			}
			wires <- wire
		}))
		cl, err := client.New(ts.URL, client.WithTransports(websocket.Default), client.WithReconnection(false))
		require.NoError(t, err)
		n := cl.Socket("/")
		n.OnConnect(func(client.Conn) error { ready <- struct{}{}; return nil })
		n.OnError(func(_ client.Conn, err error) { p.failures <- err })
		n.OnDisconnect(func(client.Conn, string) { p.disconnected <- struct{}{} })
		p.on = func(name string, f func(string), one bool) func() {
			handler := func(_ client.Conn, value string) { f(value) }
			if one {
				return n.Once(name, handler)
			}
			return n.On(name, handler)
		}
		p.off = n.Off
		p.close = func() {
			once.Do(func() {
				if p.wire != nil {
					_ = p.wire.Close()
				}
				_ = cl.Close()
				ts.Close()
			})
		}
		t.Cleanup(p.close)
		require.NoError(t, cl.Connect())
		p.wire = receiveRole(t, wires)
	}
	receiveRole(t, ready)
	return p
}

func (p *rolePeer) send(t *testing.T, name, value string) {
	t.Helper()
	payload, err := json.Marshal([]string{name, value})
	require.NoError(t, err)
	require.NoError(t, p.wire.WriteMessage(gorilla.TextMessage, append([]byte("42"), payload...)))
}

func TestRolesListenerRegistrationAndRemoval(t *testing.T) {
	for _, role := range []string{"server", "client"} {
		t.Run(role, func(t *testing.T) {
			p := newRolePeer(t, role)
			calls := make(chan string, 8)
			off := p.on("notice", func(value string) { calls <- "first:" + value }, false)
			p.on("notice", func(value string) { calls <- "once:" + value }, true)
			p.send(t, "notice", "one")
			require.Equal(t, "first:one", receiveRole(t, calls))
			require.Equal(t, "once:one", receiveRole(t, calls))
			off()
			p.on("notice", func(value string) { calls <- "second:" + value }, false)
			p.send(t, "notice", "two")
			require.Equal(t, "second:two", receiveRole(t, calls))
			p.off("notice")
			p.on("barrier", func(value string) { calls <- value }, false)
			p.send(t, "notice", "removed")
			p.send(t, "barrier", "done")
			require.Equal(t, "done", receiveRole(t, calls))
			require.Empty(t, calls)
		})
	}
}

func TestRolesFrameReaderFailureReportedOnce(t *testing.T) {
	for _, role := range []string{"server", "client"} {
		t.Run(role, func(t *testing.T) {
			p := newRolePeer(t, role)
			p.on("partial", func(string) { t.Error("partial event was dispatched") }, false)
			writer, err := p.wire.NextWriter(gorilla.TextMessage)
			require.NoError(t, err)
			_, err = writer.Write([]byte(`42["partial","` + strings.Repeat("x", 20000)))
			require.NoError(t, err)
			// Flush websocket fragments, then break TCP before the message ends.
			require.NoError(t, p.wire.NetConn().Close())
			failure := receiveRole(t, p.failures)
			var closed *gorilla.CloseError
			require.ErrorAs(t, failure, &closed)
			require.Equal(t, gorilla.CloseAbnormalClosure, closed.Code)
			receiveRole(t, p.disconnected)
			p.close()
			require.Empty(t, p.failures, "the same frame failure must not be reported twice")
		})
	}
}

func TestRolesHeaderHandoffWhileHandlerBlocks(t *testing.T) {
	for _, role := range []string{"server", "client"} {
		t.Run(role, func(t *testing.T) {
			p := newRolePeer(t, role)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			var blocked atomic.Bool
			values := make(chan string, 3)
			p.on("block", func(value string) {
				blocked.Store(true)
				close(entered)
				<-release
				values <- value
				blocked.Store(false)
			}, false)
			p.on("next", func(value string) {
				if blocked.Load() {
					t.Error("next handler ran before the blocked dispatcher returned")
				}
				values <- value
			}, false)
			p.send(t, "block", "one")
			receiveRole(t, entered)
			p.send(t, "next", "two")
			p.send(t, "next", "three")
			once.Do(func() { close(release) })
			for _, want := range []string{"one", "two", "three"} {
				require.Equal(t, want, receiveRole(t, values))
			}
		})
	}
}
