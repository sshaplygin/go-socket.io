package redis

import (
	"context"
	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	"github.com/gomodule/redigo/redis"
	"github.com/sshaplygin/go-socket.io/adapter"
	"io"
	"sync"
	"testing"
	"time"
)

func TestDatabaseOptionPreserved(t *testing.T) {
	if got := getOptions(&options{DB: 3}).DB; got != 3 {
		t.Fatalf("database = %d, want 3", got)
	}
}

// A successful command write does not mean Redis registered the subscription.
func TestSubscribeWaitsForAcknowledgements(t *testing.T) {
	c := &silentSubscription{done: make(chan struct{})}
	bc := &broadcast{pattern: "socket.io#*", reqChannel: "socket.io-request#/#", resChannel: "socket.io-response#/#"}
	dial := func(context.Context) (redis.Conn, error) { return c, nil }
	bc.dial.Store(&dial)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	sub, err := bc.subscribe(ctx)
	if sub != nil {
		_ = sub.Close()
	}
	if err == nil {
		t.Fatal("subscription returned ready without Redis acknowledgements")
	}
}

type silentSubscription struct {
	done chan struct{}
	once sync.Once
}

func (c *silentSubscription) Close() error                                   { c.once.Do(func() { close(c.done) }); return nil }
func (c *silentSubscription) Err() error                                     { return nil }
func (c *silentSubscription) Do(string, ...interface{}) (interface{}, error) { return nil, nil }
func (c *silentSubscription) Send(string, ...interface{}) error              { return nil }
func (c *silentSubscription) Flush() error                                   { return nil }
func (c *silentSubscription) Receive() (interface{}, error)                  { <-c.done; return nil, io.EOF }

func TestCloseCancelsSubscriberReconnect(t *testing.T) {
	s := miniredis.RunT(t)
	bc := newTestRedisBroadcast(t, s)
	entered := make(chan struct{})
	dial := func(ctx context.Context) (redis.Conn, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() }
	bc.dial.Store(&dial)
	bc.subLock.Lock()
	_ = bc.sub.Close()
	bc.subLock.Unlock()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("reconnect did not start")
	}
	closed := make(chan struct{})
	go func() { _ = bc.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Close waits for reconnect dial instead of canceling it")
	}
}

func TestDynamicNamespaceWaitsForSubscriptionAcknowledgements(t *testing.T) {
	s := miniredis.RunT(t)
	factory := New(WithAddr(s.Addr()))
	first, err := factory("/first")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.(*broadcast).Close() }()
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	s.Server().SetPreHook(func(_ *server.Peer, cmd string, args ...string) bool {
		if cmd == "SUBSCRIBE" && len(args) > 0 && args[0] == "socket.io-request#/new#" {
			close(entered)
			<-release
		}
		return false
	})
	type result struct {
		bc  adapter.Adapter
		err error
	}
	done := make(chan result, 1)
	go func() { bc, err := factory("/new"); done <- result{bc, err} }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("dynamic subscription not sent")
	}
	select {
	case got := <-done:
		if got.bc != nil {
			_ = got.bc.(*broadcast).Close()
		}
		t.Fatalf("namespace returned before ACK: %v", got.err)
	case <-time.After(20 * time.Millisecond):
	}
	// Release before waiting without closing the deferred signal twice.
	s.Server().SetPreHook(nil)
	release <- struct{}{}
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		_ = got.bc.(*broadcast).Close()
	case <-time.After(time.Second):
		t.Fatal("namespace not ready after ACK")
	}
}

func TestDynamicSubscriptionWriteIsBounded(t *testing.T) {
	old := redisDialTimeout
	redisDialTimeout = 40 * time.Millisecond
	defer func() { redisDialTimeout = old }()
	c := &blockedSubscriptionWrite{silentSubscription: silentSubscription{done: make(chan struct{})}}
	owner := &broadcast{done: make(chan struct{}), pattern: "socket.io#*", sub: &redis.PubSubConn{Conn: c}}
	shared := &sharedResources{owner: owner, members: make(map[string][]*broadcast)}
	owner.resources = shared
	shared.members["/old"] = []*broadcast{owner}
	owner.subscriptionChanged(owner.pattern, true)
	result := make(chan error, 1)
	go func() { _, err := shared.add("/new", &options{Prefix: "socket.io"}); result <- err }()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("blocked subscription succeeded")
		}
	case <-time.After(200 * time.Millisecond):
		_ = c.Close()
		<-result
		t.Fatal("dynamic subscription write exceeded construction deadline")
	}
}

type blockedSubscriptionWrite struct{ silentSubscription }

func (c *blockedSubscriptionWrite) Flush() error { <-c.done; return io.EOF }

func TestReadySubscriptionHonoursCanceledContext(t *testing.T) {
	bc := &broadcast{done: make(chan struct{}), pattern: "p#*", reqChannel: "p-request#/#", resChannel: "p-response#/#"}
	bc.markReady([]interface{}{bc.reqChannel, bc.resChannel})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := bc.waitReady(ctx); err != context.Canceled {
		t.Fatalf("canceled readiness returned %v", err)
	}
}
