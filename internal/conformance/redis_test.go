//go:build conformance

package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/adapter"
	redisadapter "github.com/sshaplygin/go-socket.io/adapter/redis"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/parser"
	"github.com/stretchr/testify/require"
)

type redisClusterCommands interface {
	IDsContext(context.Context, ...string) ([]string, error)
	AllRoomsContext(context.Context) ([]string, error)
	ClientRooms(context.Context, string) ([]string, error)
	RemoteJoin(context.Context, string, string) error
	RemoteLeave(context.Context, string, string) error
	RemoteDisconnect(context.Context, string, bool) error
	CustomRequest(context.Context, json.RawMessage) ([]json.RawMessage, error)
	ClearContext(context.Context, string) error
}

func redisClusterServer(t *testing.T, addr, prefix string) *httptest.Server {
	t.Helper()
	factory := redisadapter.New(redisadapter.WithAddr(addr), redisadapter.WithPrefix(prefix), redisadapter.WithRequestsTimeout(1500*time.Millisecond), redisadapter.WithCustomRequestHandler(func(ctx context.Context, data json.RawMessage) (json.RawMessage, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]interface{}{"go": prefix, "data": data})
	}))
	s := socketio.NewServer(socketio.WithAdapter(factory), socketio.WithEngineOptions(engineio.WithPerMessageDeflate(true), engineio.WithCompressionThreshold(1)))
	for _, name := range []string{"/", "/cluster", "/cluster#other", "/lifecycle"} {
		n := s.Of(name)
		n.OnConnect(func(socketio.Conn) error { return nil })
		n.OnEvent("identity", func(c socketio.Conn) string { return c.ID() })
		n.OnEvent("join", func(c socketio.Conn, room string) string { c.Join(room); return "ok" })
		n.OnEvent("leave-all", func(c socketio.Conn) string { c.LeaveAll(); return "ok" })
	}
	mux := http.NewServeMux()
	mux.Handle("/socket.io/", s)
	mux.HandleFunc("/control", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Op            string
			Namespace     string
			Rooms         []string
			Except        []string
			Local         bool
			Volatile      bool
			NoCompress    bool
			SID           string
			Room          string
			Close         bool
			TimeoutMillis int
			Data          json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if q.Namespace == "" {
			q.Namespace = "/cluster"
		}
		a := s.Of(q.Namespace).Adapter()
		timeout := 3 * time.Second
		if q.TimeoutMillis > 0 {
			timeout = time.Duration(q.TimeoutMillis) * time.Millisecond
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		var result interface{}
		var err error
		if q.Op == "broadcast" || q.Op == "binary" {
			value := interface{}("payload")
			if q.Op == "binary" {
				value = map[string]interface{}{"bytes": parser.Buffer{Data: []byte{0, 255, 2}}, "nested": []interface{}{parser.Buffer{Data: []byte{3, 254}}}}
			}
			err = a.SendTo(adapter.Options{Rooms: q.Rooms, Except: q.Except, Local: q.Local, Volatile: q.Volatile, NoCompress: q.NoCompress}, q.Op, value)
		} else if c, ok := a.(redisClusterCommands); !ok {
			err = fmt.Errorf("adapter lacks cluster commands")
		} else {
			switch q.Op {
			case "ids":
				result, err = c.IDsContext(ctx, q.Rooms...)
			case "rooms":
				result, err = c.AllRoomsContext(ctx)
			case "clientRooms":
				result, err = c.ClientRooms(ctx, q.SID)
			case "join":
				err = c.RemoteJoin(ctx, q.SID, q.Room)
			case "leave":
				err = c.RemoteLeave(ctx, q.SID, q.Room)
			case "disconnect":
				err = c.RemoteDisconnect(ctx, q.SID, q.Close)
			case "custom":
				result, err = c.CustomRequest(ctx, q.Data)
			case "clear":
				err = c.ClearContext(ctx, q.Room)
			default:
				err = fmt.Errorf("unknown operation %q", q.Op)
			}
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
	served := make(chan error, 1)
	go func() { served <- s.Serve() }()
	ts := httptest.NewServer(mux)
	t.Cleanup(func() { require.NoError(t, s.Close()); ts.Close(); require.NoError(t, <-served) })
	return ts
}

func TestRedisMixedCluster(t *testing.T) {
	addr := os.Getenv("CONFORMANCE_REDIS_ADDR")
	require.NotEmpty(t, addr, "CONFORMANCE_REDIS_ADDR must name a real Redis service")
	for _, transport := range []string{"polling", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			prefix := "socketio-conformance-" + uuid.NewString()
			a := redisClusterServer(t, addr, prefix)
			b := redisClusterServer(t, addr, prefix)
			runNode(t, "testdata/node/server/redis-cluster.js", a.URL, b.URL, addr, prefix, transport)
		})
	}
}
