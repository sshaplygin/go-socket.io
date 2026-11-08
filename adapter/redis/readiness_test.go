package redis

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	"github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
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
	var dials atomic.Int32
	var enter sync.Once
	// A dial after Close must fail the assertion below, not panic the binary.
	dial := func(ctx context.Context) (redis.Conn, error) {
		dials.Add(1)
		enter.Do(func() { close(entered) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
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
	// The canceled attempt doubles the backoff to 2*redisReconnectMin; wait
	// past redisReconnectMax for a further attempt.
	require.Never(t, func() bool { return dials.Load() > 1 }, redisReconnectMax, time.Millisecond,
		"subscriber dialed again after Close")
}

// Once done is closed the reconnect loop must not dial again, even when the
// backoff timer is ready at the same time and transportCtx is not yet
// canceled (closeTransport closes done before it cancels transportCtx).
func TestResubscribeAfterCloseDoesNotDial(t *testing.T) {
	if os.Getenv(syncTimerChildEnv) != "1" {
		// With synchronous timer channels (Go 1.23 semantics) an expired
		// timer is ready in the same select as done. Go 1.23 to 1.26 give
		// this module's own tests asynchronous channels because of its go
		// 1.22 line (Go 1.27 removed them, Go 1.22 has only those), so repeat
		// the check in a child with synchronous ones.
		require.NoError(t, runSyncTimerChild(t.Name()))
	}
	var dials atomic.Int32
	dial := func(context.Context) (redis.Conn, error) { dials.Add(1); return nil, io.EOF }
	for i := 0; i < 500; i++ {
		bc := &broadcast{done: make(chan struct{}), transportCtx: context.Background(), pattern: "p#*", reqChannel: "p-request#/#", resChannel: "p-response#/#"}
		bc.dial.Store(&dial)
		close(bc.done)
		// A zero delay keeps the timer ready on every attempt.
		if sub, _ := bc.resubscribe(0); sub != nil {
			t.Fatal("closed broadcast resubscribed")
		}
	}
	if n := dials.Load(); n != 0 {
		t.Fatalf("closed broadcast dialed %d times", n)
	}
}

// syncTimerChildEnv marks a child test binary started by runSyncTimerChild.
const syncTimerChildEnv = "SOCKETIO_SYNC_TIMER_CHILD"

// runSyncTimerChild runs the test name in a child test binary with
// synchronous timer channels. It fails unless the child ran that test and
// the test passed.
func runSyncTimerChild(name string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	args := []string{"-test.run=^" + regexp.QuoteMeta(name) + "$", "-test.count=1", "-test.timeout=30s", "-test.v"}
	// Under go test -cover the child adds its counters to the directory this
	// binary reads its own from when it writes the profile.
	if dir := flag.Lookup("test.gocoverdir"); dir != nil && dir.Value.String() != "" {
		args = append(args, "-test.gocoverdir="+dir.Value.String())
	}
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = append(os.Environ(), syncTimerChildEnv+"=1", "GODEBUG=asynctimerchan=0")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("synchronous timer child %s: %w\n%s", name, err, output)
	}
	return childPassed(name, output)
}

// childPassed checks the verbose output of a child test binary for the
// result of the test name: a pattern that matched no test, or a skipped
// test, also exits successfully.
func childPassed(name string, output []byte) error {
	if !bytes.Contains(output, []byte("\n--- PASS: "+name+" (")) {
		return fmt.Errorf("synchronous timer child did not pass %s:\n%s", name, output)
	}
	return nil
}

// A child whose pattern matches no test exits successfully without running
// anything.
func TestSyncTimerChildFailsWithoutTest(t *testing.T) {
	require.Error(t, runSyncTimerChild("TestNoSuchTest"), "a child that ran no test passed")
}

func TestSyncTimerChildRequiresPassResult(t *testing.T) {
	const name = "TestResubscribeAfterCloseDoesNotDial"
	for _, output := range []string{
		"testing: warning: no tests to run\nPASS\n",
		"=== RUN   " + name + "\n--- SKIP: " + name + " (0.00s)\nPASS\n",
		"=== RUN   " + name + "Twice\n--- PASS: " + name + "Twice (0.00s)\nPASS\n",
	} {
		require.Error(t, childPassed(name, []byte(output)), output)
	}
	require.NoError(t, childPassed(name, []byte("=== RUN   "+name+"\n--- PASS: "+name+" (0.01s)\nPASS\n")))
}

func TestSubscribeWithCanceledContextDoesNotDial(t *testing.T) {
	var dials atomic.Int32
	dial := func(context.Context) (redis.Conn, error) { dials.Add(1); return nil, io.EOF }
	bc := &broadcast{pattern: "p#*", reqChannel: "p-request#/#", resChannel: "p-response#/#"}
	bc.dial.Store(&dial)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bc.subscribeChannels(ctx, []interface{}{bc.reqChannel, bc.resChannel}); err != context.Canceled {
		t.Fatalf("canceled subscription returned %v", err)
	}
	if n := dials.Load(); n != 0 {
		t.Fatalf("canceled subscription dialed %d times", n)
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
	c := &blockedSubscriptionWrite{silentSubscription: silentSubscription{done: make(chan struct{})}}
	owner := &broadcast{done: make(chan struct{}), pattern: "socket.io#*", sub: &redis.PubSubConn{Conn: c}}
	shared := &sharedResources{owner: owner, members: make(map[string][]*broadcast)}
	owner.resources = shared
	shared.members["/old"] = []*broadcast{owner}
	owner.subscriptionChanged(owner.pattern, true)
	result := make(chan error, 1)
	go func() {
		_, err := shared.add("/new", &options{Prefix: "socket.io", SubscribeTimeout: time.Second})
		result <- err
	}()
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		_ = c.Close()
		err := <-result
		t.Fatalf("construction deadline did not close the blocked subscriber: %v", err)
	}
	// Cleanup shares the expired construction deadline; it must not start
	// another full timeout while waiting for unsubscribe confirmation.
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("blocked subscription succeeded")
		}
	case <-time.After(200 * time.Millisecond):
		_ = c.Close()
		err := <-result
		t.Fatalf("dynamic subscription write exceeded construction deadline: %v", err)
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

func TestNamespaceCloseBoundsUnsubscribeWrite(t *testing.T) {
	c := &blockedSubscriptionWrite{silentSubscription: silentSubscription{done: make(chan struct{})}}
	owner := &broadcast{done: make(chan struct{}), pattern: "socket.io#*", sub: &redis.PubSubConn{Conn: c}}
	member := &broadcast{subscribeTimeout: 40 * time.Millisecond, nsp: "/closing", reqChannel: "socket.io-request#/closing#", resChannel: "socket.io-response#/closing#"}
	shared := &sharedResources{owner: owner, members: map[string][]*broadcast{"/remaining": {owner}, "/closing": {member}}}
	owner.resources = shared
	member.resources = shared
	done := make(chan error, 1)
	go func() { done <- member.Close() }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.ErrorIs(t, err, io.EOF)
	case <-time.After(200 * time.Millisecond):
		_ = c.Close()
		err := <-done
		t.Fatalf("namespace Close hangs in unsubscribe flush: %v", err)
	}
	owner.subLock.Lock()
	if owner.sub == nil {
		t.Error("shared subscriber reference lost during member close")
	}
	owner.subLock.Unlock()
	shared.mu.Lock()
	_, retained := shared.members["/closing"]
	shared.mu.Unlock()
	if retained {
		t.Fatal("closed namespace remains registered")
	}
}

func TestNamespaceCloseWaitsForUnsubscribeAcknowledgements(t *testing.T) {
	s := miniredis.RunT(t)
	factory := New(WithAddr(s.Addr()))
	closing, err := factory("/closing")
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := factory("/remaining")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closing.(*broadcast).Close(); _ = remaining.(*broadcast).Close() }()
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	s.Server().SetPreHook(func(_ *server.Peer, command string, args ...string) bool {
		if command == "UNSUBSCRIBE" && len(args) > 0 && args[0] == "socket.io-request#/closing#" {
			close(entered)
			<-release
		}
		return false
	})
	done := make(chan error, 1)
	go func() { done <- closing.(*broadcast).Close() }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("unsubscribe was not sent")
	}
	var returned bool
	var closeErr error
	select {
	case closeErr = <-done:
		returned = true
	case <-time.After(20 * time.Millisecond):
	}
	unblock.Do(func() { close(release) })
	if !returned {
		select {
		case closeErr = <-done:
		case <-time.After(time.Second):
			t.Fatal("Close did not complete after unsubscribe ACK")
		}
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if returned {
		t.Fatal("Close returned before Redis processed unsubscribe")
	}
	if got := s.PubSubNumSub("socket.io-request#/closing#")["socket.io-request#/closing#"]; got != 0 {
		t.Fatalf("closed namespace still counted as %d subscribers", got)
	}
}

func TestNamespaceCloseConfirmsRemovalAfterSubscriberReplacement(t *testing.T) {
	s := miniredis.RunT(t)
	factory := New(WithAddr(s.Addr()))
	closing, err := factory("/closing")
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := factory("/remaining")
	if err != nil {
		t.Fatal(err)
	}
	owner := closing.(*broadcast)
	defer func() { _ = owner.Close(); _ = remaining.(*broadcast).Close() }()
	sent := make(chan struct{})
	s.Server().SetPreHook(func(_ *server.Peer, command string, args ...string) bool {
		if command == "UNSUBSCRIBE" && len(args) > 0 && args[0] == owner.reqChannel {
			close(sent)
			return true
		}
		return false
	})
	done := make(chan error, 1)
	go func() { done <- owner.Close() }()
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("unsubscribe not sent")
	}
	owner.subLock.Lock()
	_ = owner.sub.Close()
	owner.subLock.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("replacement did not confirm retired subscription removal")
	}
	if got := s.PubSubNumSub(owner.reqChannel)[owner.reqChannel]; got != 0 {
		t.Fatalf("replacement retained closed namespace: %d", got)
	}
	if got := s.PubSubNumSub("socket.io-request#/remaining#")["socket.io-request#/remaining#"]; got != 1 {
		t.Fatalf("remaining namespace subscriber count %d", got)
	}
}

type failedUnsubscribeConn struct{ redis.Conn }

func (c *failedUnsubscribeConn) Send(command string, args ...interface{}) error {
	if command == "UNSUBSCRIBE" {
		return io.ErrClosedPipe
	}
	return c.Conn.Send(command, args...)
}

func TestNamespaceCloseRecoversFromUnsubscribeWriteFailure(t *testing.T) {
	s := miniredis.RunT(t)
	factory := New(WithAddr(s.Addr()))
	closing, err := factory("/closing")
	require.NoError(t, err)
	remaining, err := factory("/remaining")
	require.NoError(t, err)
	owner := closing.(*broadcast)
	t.Cleanup(func() { _ = owner.Close(); _ = remaining.(*broadcast).Close() })

	dial := *owner.dial.Load()
	failUnsubscribe := func(ctx context.Context) (redis.Conn, error) {
		conn, err := dial(ctx)
		if err != nil {
			return nil, err
		}
		return &failedUnsubscribeConn{Conn: conn}, nil
	}
	owner.dial.Store(&failUnsubscribe)
	owner.subLock.Lock()
	previous := owner.sub
	_ = previous.Close()
	owner.subLock.Unlock()
	require.Eventually(t, func() bool {
		owner.subLock.Lock()
		defer owner.subLock.Unlock()
		return owner.sub != previous
	}, 2*time.Second, time.Millisecond, "subscriber did not reconnect")

	// A failed write must close that transport and await a replacement which
	// subscribes only the remaining namespace before reporting success.
	require.NoError(t, owner.Close())
	require.Zero(t, s.PubSubNumSub(owner.reqChannel)[owner.reqChannel])
	channel := "socket.io-request#/remaining#"
	require.Equal(t, 1, s.PubSubNumSub(channel)[channel])
}

func TestUnsubscribeAcknowledgementsConfirmOneCommandAtATime(t *testing.T) {
	bc := &broadcast{}
	sub := &redis.PubSubConn{}
	first := bc.beginUnsubscribe(sub, "request", "response")
	second := bc.beginUnsubscribe(sub, "request", "response")
	bc.acknowledgeUnsubscribe(sub, "request")
	bc.acknowledgeUnsubscribe(sub, "response")
	select {
	case <-first.done:
	default:
		t.Fatal("first unsubscribe not confirmed")
	}
	select {
	case <-second.done:
		t.Fatal("one Redis ACK confirmed two unsubscribe commands")
	default:
	}
	bc.acknowledgeUnsubscribe(sub, "request")
	bc.acknowledgeUnsubscribe(sub, "response")
	select {
	case <-second.done:
	default:
		t.Fatal("second unsubscribe not confirmed")
	}
}

// Each subscriber reconnect attempt is bounded by WithSubscribeTimeout. With
// every PSUBSCRIBE held, the 250ms attempts time out and repeat after the test
// backoff (at most redisReconnectMax); a ten-second bound would allow one
// attempt within 5s.
func TestReconnectAttemptBoundedBySubscribeTimeout(t *testing.T) {
	s := miniredis.RunT(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	bc := newTestRedisBroadcast(t, s, func(o *options) { o.SubscribeTimeout = 250 * time.Millisecond })
	var attempts atomic.Int32
	s.Server().SetPreHook(func(_ *server.Peer, command string, _ ...string) bool {
		if command == "PSUBSCRIBE" {
			attempts.Add(1)
			<-release
		}
		return false
	})
	bc.subLock.Lock()
	_ = bc.sub.Close()
	bc.subLock.Unlock()
	require.Eventually(t, func() bool { return attempts.Load() >= 3 }, 5*time.Second, 5*time.Millisecond,
		"reconnect attempts are not bounded by WithSubscribeTimeout")
}

func TestNonpositiveSubscribeTimeoutSelectsDefault(t *testing.T) {
	for _, value := range []time.Duration{0, -time.Second} {
		require.Equal(t, 10*time.Second, getOptions(&options{SubscribeTimeout: value}).subscribeTimeout(), value)
		require.Equal(t, 10*time.Second, (&broadcast{subscribeTimeout: value}).subscribeTimeoutOrDefault(), value)
	}
}
