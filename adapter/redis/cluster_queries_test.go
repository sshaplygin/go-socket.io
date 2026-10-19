package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/alicebob/miniredis/v2/server"
	redigo "github.com/gomodule/redigo/redis"
	"github.com/sshaplygin/go-socket.io/adapter"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

// Covers A5
// Covers S13
func TestRedisClusterIDsIncludesRemoteRooms(t *testing.T) {
	s := miniredis.RunT(t)
	a, b := newTestRedisBroadcast(t, s), newTestRedisBroadcast(t, s)
	a.Join("one", newRedisTestConn("a"))
	b.Join("two", newRedisTestConn("b"))
	b.Join("one", newRedisTestConn("c"))
	require.ElementsMatch(t, []string{"a", "b", "c"}, a.IDs("one", "two"))
}

type clusterTestSocket struct {
	*redisTestConn
	owner        *broadcast
	disconnected chan bool
}

func newClusterTestSocket(owner *broadcast, id string) *clusterTestSocket {
	return &clusterTestSocket{redisTestConn: newRedisTestConn(id), owner: owner, disconnected: make(chan bool, 2)}
}
func (s *clusterTestSocket) Join(rooms ...string) {
	for _, room := range rooms {
		s.owner.Memory.Join(room, s)
	}
}
func (s *clusterTestSocket) Leave(room string) { s.owner.Memory.Leave(room, s) }
func (s *clusterTestSocket) Disconnect()       { s.owner.Memory.LeaveAll(s); s.disconnected <- false }
func (s *clusterTestSocket) Close() error {
	s.owner.Memory.LeaveAll(s)
	s.disconnected <- true
	return nil
}

// Covers A5
// Covers S13
// Covers A9
func TestRedisClusterQueriesAndRemoteOperations(t *testing.T) {
	s := miniredis.RunT(t)
	a, b := newTestRedisBroadcast(t, s), newTestRedisBroadcast(t, s)
	socket := newClusterTestSocket(b, "remote")
	socket.Join("initial")
	b.BindSocketLookup(func(id string) adapter.Socket {
		if id == socket.ID() {
			return socket
		}
		return nil
	})
	ctx := context.Background()
	require.NoError(t, a.RemoteJoin(ctx, "remote", "joined"))
	ids, err := a.IDsContext(ctx, "joined")
	require.NoError(t, err)
	require.Equal(t, []string{"remote"}, ids)
	count, err := a.LenContext(ctx, "joined")
	require.NoError(t, err)
	require.Equal(t, 1, count)
	rooms, err := a.AllRoomsContext(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"initial", "joined"}, rooms)
	rooms, err = a.ClientRooms(ctx, "remote")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"initial", "joined"}, rooms)
	require.NoError(t, a.ClearContext(ctx, "joined"))
	require.Zero(t, b.Memory.Len("joined"))
	require.NoError(t, a.RemoteLeave(ctx, "remote", "initial"))
	require.Empty(t, socket.owner.Memory.AllRooms())
	// Live registry lookup still works after LeaveAll removed every index entry.
	require.NoError(t, a.RemoteJoin(ctx, "remote", "again"))
	for _, closeEngine := range []bool{false, true} {
		require.NoError(t, a.RemoteDisconnect(ctx, "remote", closeEngine))
		require.Equal(t, closeEngine, <-socket.disconnected)
	}
	b.BindSocketLookup(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, a.RemoteJoin(ctx, "missing", "room"), context.DeadlineExceeded)
}

// Covers A9
// Covers B4
func TestRedisClusterRequestTimeoutCancellationAndClose(t *testing.T) {
	s := miniredis.RunT(t)
	factory := New(WithAddr(s.Addr()), WithRequestsTimeout(25*time.Millisecond))
	base, err := factory("/")
	require.NoError(t, err)
	a := base.(*broadcast)
	t.Cleanup(func() { _ = a.Close() })
	silent, err := redigo.Dial("tcp", s.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = silent.Close() })
	require.NoError(t, redigo.PubSubConn{Conn: silent}.Subscribe(a.reqChannel))
	waitRedisSubscribers(t, s, a.reqChannel, 2)
	a.Join("local", newRedisTestConn("local"))
	ids, err := a.IDsContext(context.Background())
	require.ErrorIs(t, err, ErrRequestTimeout)
	require.Equal(t, []string{"local"}, ids)
	require.Equal(t, -1, a.Len("local"))
	rooms, err := a.AllRoomsContext(context.Background())
	require.ErrorIs(t, err, ErrRequestTimeout)
	require.Equal(t, []string{"local"}, rooms)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = a.IDsContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
	done := make(chan error, 1)
	go func() { _, err := a.CustomRequest(context.Background(), nil); done <- err }()
	require.Eventually(t, func() bool { a.requestLock.Lock(); defer a.requestLock.Unlock(); return len(a.requests) > 0 }, time.Second, time.Millisecond)
	require.NoError(t, a.Close())
	require.ErrorIs(t, <-done, ErrAdapterClosed)
	_, err = a.IDsContext(context.Background())
	require.ErrorIs(t, err, ErrAdapterClosed)
	a.requestLock.Lock()
	require.Empty(t, a.requests)
	a.requestLock.Unlock()
}

// Covers A9
func TestRedisClusterCustomRequestsSharedAndReentrant(t *testing.T) {
	s := miniredis.RunT(t)
	var calls atomic.Int32
	factory := New(WithAddr(s.Addr()), WithRequestsTimeout(time.Second), WithCustomRequestHandler(func(ctx context.Context, data json.RawMessage) (json.RawMessage, error) {
		calls.Add(1)
		return data, nil
	}))
	first, err := factory("/")
	require.NoError(t, err)
	second, err := factory("/")
	require.NoError(t, err)
	a, b := first.(*broadcast), second.(*broadcast)
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	a.Join("one", newRedisTestConn("a"))
	b.Join("two", newRedisTestConn("b"))
	ids, err := a.IDsContext(context.Background())
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"a", "b"}, ids)
	replies, err := a.CustomRequest(context.Background(), json.RawMessage(`{"integer":9007199254740993}`))
	require.NoError(t, err)
	require.Equal(t, []json.RawMessage{json.RawMessage(`{"integer":9007199254740993}`)}, replies)
	require.EqualValues(t, 1, calls.Load())
	_, err = a.CustomRequest(context.Background(), json.RawMessage(`broken`))
	require.Error(t, err)
	defaults, err := New(WithAddr(s.Addr()), WithRequestsTimeout(-1), WithCustomRequestHandler(nil))("/default")
	require.NoError(t, err)
	defer func() { _ = defaults.(*broadcast).Close() }()
	replies, err = defaults.(Cluster).CustomRequest(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, []json.RawMessage{json.RawMessage("null")}, replies)
}

// Covers A9
func TestRedisClusterMalformedResponseAndGoDeduplication(t *testing.T) {
	s := miniredis.RunT(t)
	a := newTestRedisBroadcast(t, s)
	pending := &clusterRequest{kind: clientsRequest, expected: 2, done: make(chan struct{}), values: map[string]bool{}, responders: map[string]bool{}}
	a.setRequest("pending", pending)
	for _, raw := range []string{`{"requestid":"pending","clients":null}`, `{"requestid":"pending","clients":[1]}`, `{"requestid":"pending","rooms":[]}`, `{"requestid":"pending","clients":[],"responder":1}`} {
		a.onResponse([]byte(raw))
	}
	require.Zero(t, pending.received)
	a.onResponse([]byte(`{"requestid":"pending","clients":["a"],"responder":"one"}`))
	a.onResponse([]byte(`{"requestid":"pending","clients":["a"],"responder":"one"}`))
	require.Equal(t, 1, pending.received)
	a.onResponse([]byte(`{"requestid":"pending","clients":["b"]}`))
	require.Equal(t, 2, pending.received)
	a.onResponse([]byte(`{"requestid":"pending","clients":["late"]}`))
	require.NotContains(t, pending.values, "late")
}

// Covers A5
// Covers B4
func TestRedisClusterOutageRetainsLocalPartialSnapshot(t *testing.T) {
	s := miniredis.RunT(t)
	a := newTestRedisBroadcast(t, s)
	a.Join("local", newRedisTestConn("local"))
	a.subscriptionChanged("", false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	ids, err := a.IDsContext(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, []string{"local"}, ids)
}

// Covers A9
func TestRedisCustomHookIsReentrantAndWorkersAreBounded(t *testing.T) {
	s := miniredis.RunT(t)
	var a *broadcast
	factory := New(WithAddr(s.Addr()), WithCustomRequestHandler(func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		ids, err := a.IDsContext(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(ids)
	}))
	base, err := factory("/")
	require.NoError(t, err)
	a = base.(*broadcast)
	t.Cleanup(func() { _ = a.Close() })
	a.Join("room", newRedisTestConn("id"))
	replies, err := a.CustomRequest(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, []json.RawMessage{json.RawMessage(`["id"]`)}, replies)
	entered, ended := make(chan struct{}, 32), make(chan struct{}, 32)
	blockedFactory := New(WithAddr(s.Addr()), WithRequestsTimeout(time.Second), WithCustomRequestHandler(func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		entered <- struct{}{}
		<-ctx.Done()
		ended <- struct{}{}
		return nil, ctx.Err()
	}))
	blockedBase, err := blockedFactory("/blocked")
	require.NoError(t, err)
	blocked := blockedBase.(*broadcast)
	t.Cleanup(func() { _ = blocked.Close() })
	for i := 0; i < 33; i++ {
		raw := fmt.Sprintf(`{"type":5,"requestid":"%d","data":null}`, i)
		blocked.onRequest([]byte(raw))
	}
	for i := 0; i < 32; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("worker did not start")
		}
	}
	require.Len(t, blocked.resources.clusterWorkers(), 32)
	require.NoError(t, blocked.Close())
	for i := 0; i < 32; i++ {
		select {
		case <-ended:
		case <-time.After(time.Second):
			t.Fatal("close did not cancel custom worker")
		}
	}
	require.Eventually(t, func() bool { return len(blocked.resources.clusterWorkers()) == 0 }, time.Second, time.Millisecond)
}

// Covers B4
func TestRedisClusterCommandCancellationClosesRawConnection(t *testing.T) {
	for _, closeAdapter := range []bool{false, true} {
		t.Run(fmt.Sprint(closeAdapter), func(t *testing.T) {
			s := miniredis.RunT(t)
			a := newTestRedisBroadcast(t, s)
			seen := make(chan struct{})
			var once sync.Once
			s.Server().SetPreHook(func(_ *server.Peer, command string, _ ...string) bool {
				if command == "PUBSUB" {
					once.Do(func() { close(seen) })
					return true
				}
				return false
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := a.IDsContext(ctx); done <- err }()
			select {
			case <-seen:
			case <-time.After(time.Second):
				t.Fatal("NUMSUB not issued")
			}
			if closeAdapter {
				require.NoError(t, a.Close())
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if closeAdapter {
					require.ErrorIs(t, err, ErrAdapterClosed)
				} else {
					require.ErrorIs(t, err, context.Canceled)
				}
			case <-time.After(time.Second):
				t.Fatal("command did not terminate")
			}
			want := 2
			if closeAdapter {
				want = 0
			}
			require.Eventually(t, func() bool { return s.CurrentConnectionCount() == want }, time.Second, time.Millisecond)
		})
	}
}

// Covers A9
func TestRedisUnknownSocketAndMissingLifecycleCapabilityNeverSucceed(t *testing.T) {
	s := miniredis.RunT(t)
	a := newTestRedisBroadcast(t, s)
	a.Join("room", newRedisTestConn("unsupported"))
	for _, call := range []func(context.Context) error{
		func(ctx context.Context) error { return a.RemoteJoin(ctx, "unsupported", "new") },
		func(ctx context.Context) error { return a.RemoteLeave(ctx, "unsupported", "room") },
		func(ctx context.Context) error { return a.RemoteDisconnect(ctx, "unsupported", false) },
		func(ctx context.Context) error { return a.RemoteDisconnect(ctx, "unsupported", true) },
		func(ctx context.Context) error { _, err := a.ClientRooms(ctx, "missing"); return err },
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		err := call(ctx)
		cancel()
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}
	require.Equal(t, 1, a.Memory.Len("room"))
}

// Covers A9
func TestRedisClusterCustomHandlerPanicDoesNotCrash(t *testing.T) {
	const child = "SOCKETIO_REDIS_PANIC_CHILD"
	if os.Getenv(child) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRedisClusterCustomHandlerPanicDoesNotCrash$", "-test.v")
		cmd.Env = append(os.Environ(), child+"=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "child failed: %s", output)
		return
	}
	s := miniredis.RunT(t)
	base, err := New(WithAddr(s.Addr()), WithRequestsTimeout(15*time.Millisecond), WithCustomRequestHandler(func(context.Context, json.RawMessage) (json.RawMessage, error) { panic("hook panic") }))("/")
	require.NoError(t, err)
	a := base.(*broadcast)
	defer func() { _ = a.Close() }()
	for i := 0; i < 2; i++ {
		_, err = a.CustomRequest(context.Background(), nil)
		require.ErrorIs(t, err, ErrRequestTimeout)
	}
	require.Eventually(t, func() bool { return len(a.resources.clusterWorkers()) == 0 }, time.Second, time.Millisecond)
	_, err = a.IDsContext(context.Background())
	require.NoError(t, err, "panic must not stop Redis dispatcher")
}

// Covers A9
func TestRedisCustomHandlerFailureCannotFabricateSuccess(t *testing.T) {
	for _, handler := range []func(context.Context, json.RawMessage) (json.RawMessage, error){
		func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, fmt.Errorf("rejected") },
		func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage("invalid"), nil
		},
	} {
		s := miniredis.RunT(t)
		base, err := New(WithAddr(s.Addr()), WithRequestsTimeout(15*time.Millisecond), WithCustomRequestHandler(handler))("/")
		require.NoError(t, err)
		a := base.(*broadcast)
		t.Cleanup(func() { _ = a.Close() })
		_, err = a.CustomRequest(context.Background(), nil)
		require.ErrorIs(t, err, ErrRequestTimeout)
		require.Empty(t, a.resources.clusterWorkers())
	}
}

// Covers A5
// Covers B4
func TestRedisQueryCommandFailuresPreserveLocalStateAndRemovePending(t *testing.T) {
	cases := []struct {
		name    string
		command string
		reply   func(*server.Peer, string)
	}{
		{"ACL denies NUMSUB", "PUBSUB", func(p *server.Peer, _ string) { p.WriteError("NOPERM denied") }},
		{"short count reply", "PUBSUB", func(p *server.Peer, _ string) { p.WriteLen(0) }},
		{"wrong channel", "PUBSUB", func(p *server.Peer, _ string) { p.WriteLen(2); p.WriteBulk("wrong"); p.WriteInt(1) }},
		{"negative count", "PUBSUB", func(p *server.Peer, ch string) { p.WriteLen(2); p.WriteBulk(ch); p.WriteInt(-1) }},
		{"zero subscribers", "PUBSUB", func(p *server.Peer, ch string) { p.WriteLen(2); p.WriteBulk(ch); p.WriteInt(0) }},
		{"ACL denies PUBLISH", "PUBLISH", func(p *server.Peer, _ string) { p.WriteError("NOPERM denied") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := miniredis.RunT(t)
			a := newTestRedisBroadcast(t, s)
			a.Join("local", newRedisTestConn("local"))
			s.Server().SetPreHook(func(p *server.Peer, command string, _ ...string) bool {
				if command == tc.command {
					tc.reply(p, a.reqChannel)
					return true
				}
				return false
			})
			ids, err := a.IDsContext(context.Background())
			require.Error(t, err)
			require.Equal(t, []string{"local"}, ids)
			a.requestLock.Lock()
			require.Empty(t, a.requests)
			a.requestLock.Unlock()
		})
	}
}

// Covers A9
func TestRedisMalformedRequestsAndRepliesDoNotStopDispatcher(t *testing.T) {
	s := miniredis.RunT(t)
	done := make(chan struct{})
	base, err := New(WithAddr(s.Addr()), WithCustomRequestHandler(func(context.Context, json.RawMessage) (json.RawMessage, error) { close(done); return nil, nil }))("/")
	require.NoError(t, err)
	a := base.(*broadcast)
	t.Cleanup(func() { _ = a.Close() })
	socket := newClusterTestSocket(a, "live")
	socket.Join("keep")
	for _, raw := range []string{`invalid`, `{}`, `{"requestid":"x","type":-1}`, `{"requestid":"x","type":7}`, `{"requestid":"x","type":"0"}`, `{"requestid":"x","type":3}`, `{"requestid":"x","type":3,"sid":"live"}`, `{"requestid":"x","type":4,"sid":"live"}`} {
		s.Publish(a.reqChannel, raw)
	}
	for _, raw := range []string{`invalid`, `{"requestid":2}`, `{"requestid":"unknown","data":null}`} {
		s.Publish(a.resChannel, raw)
	}
	ids, err := a.IDsContext(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"live"}, ids)
	require.Equal(t, 1, a.Memory.Len("keep"))
	// Default hook accepts a Node request omitting its optional data field.
	s.Publish(a.reqChannel, `{"requestid":"default","type":5}`)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("default data hook not invoked")
	}
}

// Covers A9
func TestRedisClearReportsPartialFailure(t *testing.T) {
	s := miniredis.RunT(t)
	a := newTestRedisBroadcast(t, s)
	supported := newClusterTestSocket(a, "a-supported")
	supported.Join("room")
	a.Join("room", newRedisTestConn("z-unsupported"))
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, a.ClearContext(ctx, "room"), context.DeadlineExceeded)
	require.Equal(t, []string{"z-unsupported"}, a.Memory.IDs("room"), "successful earlier removal is not rolled back")
}
