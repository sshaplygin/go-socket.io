package redis

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/logger"
)

const (
	msgSubscriptionLost     = "socketio: redis subscription lost"
	msgSubscriptionRestored = "socketio: redis subscription restored"
	msgResubscribeFailed    = "socketio: redis resubscribe failed"
	msgRequestFailed        = "socketio: redis cluster request failed"
)

// logRecorder keeps every record logged through slog.Default.
type logRecorder struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (r *logRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (r *logRecorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recs = append(r.recs, rec.Clone())
	return nil
}
func (r *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *logRecorder) WithGroup(string) slog.Handler      { return r }

// records returns the records with message msg kept after the first n records.
func (r *logRecorder) records(n int, msg string) []slog.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []slog.Record
	for _, rec := range r.recs[n:] {
		if rec.Message == msg {
			out = append(out, rec)
		}
	}
	return out
}

func (r *logRecorder) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.recs)
}

// captureRedisLogs records the fallback logger with every level enabled until
// Cleanup. The test must not run in parallel.
//
// The fallback logger is process-wide, and Close does not wait for an
// adapter's dispatcher and request workers: one of an adapter that an earlier
// test closed can still log, such as the DEBUG record of a reply whose PUBLISH
// returned after that Close. Recording therefore starts once the library
// goroutines of earlier tests have ended, so every record belongs to this test.
func captureRedisLogs(t *testing.T) *logRecorder {
	t.Helper()
	if leaks := awaitModuleGoroutines(); len(leaks) > 0 {
		t.Fatalf("library goroutines of earlier tests remain:\n%s", strings.Join(leaks, "\n\n"))
	}
	rec := &logRecorder{}
	prev, prevOut, prevFlags, prevLevel := slog.Default(), log.Writer(), log.Flags(), logger.Level.Level()
	slog.SetDefault(slog.New(rec))
	logger.Level.Set(logger.LevelTrace)
	t.Cleanup(func() {
		logger.Level.Set(prevLevel)
		slog.SetDefault(prev)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return rec
}

// requireLevel checks that every record has level lvl and a non-empty err
// attribute when withErr is set.
func requireLevel(t *testing.T, recs []slog.Record, lvl slog.Level, withErr bool) {
	t.Helper()
	for _, r := range recs {
		require.Equal(t, lvl, r.Level, r.Message)
		var errText string
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "err" {
				errText = a.Value.String()
			}
			return true
		})
		if withErr {
			require.NotEmpty(t, errText, r.Message)
		}
	}
}

// requireQuiet waits several reconnect delays and checks that no subscription
// record was logged after the first n records.
func requireQuiet(t *testing.T, rec *logRecorder, n int) {
	t.Helper()
	time.Sleep(4 * redisReconnectMax)
	for _, msg := range []string{msgSubscriptionLost, msgSubscriptionRestored, msgResubscribeFailed} {
		require.Empty(t, rec.records(n, msg), msg)
	}
}

// A subscription lost for several reconnect attempts logs one WARN with the
// receive error, one DEBUG per failed attempt and one INFO on recovery.
func TestSubscriptionLossAndRecoveryLoggedOncePerTransition(t *testing.T) {
	rec := captureRedisLogs(t)
	s := miniredis.RunT(t)
	bc := newTestRedisBroadcast(t, s)
	requireQuiet(t, rec, 0)

	s.Close()
	require.Eventually(t, func() bool { return len(rec.records(0, msgResubscribeFailed)) >= 3 },
		5*time.Second, time.Millisecond, "the subscriber must keep retrying while Redis is down")
	lost := rec.records(0, msgSubscriptionLost)
	require.Len(t, lost, 1, "one record for the loss, not one per attempt")
	requireLevel(t, lost, slog.LevelWarn, true)
	requireLevel(t, rec.records(0, msgResubscribeFailed), slog.LevelDebug, true)
	require.Empty(t, rec.records(0, msgSubscriptionRestored))

	require.NoError(t, s.Restart())
	require.Eventually(t, func() bool { return len(rec.records(0, msgSubscriptionRestored)) == 1 },
		5*time.Second, time.Millisecond, "the recovery must be logged")
	requireLevel(t, rec.records(0, msgSubscriptionRestored), slog.LevelInfo, false)
	require.Len(t, rec.records(0, msgSubscriptionLost), 1)
	waitRedisSubscribers(t, s, testRedisReqChannel, 1)

	// A second loss is a new transition.
	n := rec.len()
	replaceSubscriber(t, bc)
	require.Eventually(t, func() bool { return len(rec.records(n, msgSubscriptionRestored)) == 1 },
		5*time.Second, time.Millisecond)
	require.Len(t, rec.records(n, msgSubscriptionLost), 1)

	n = rec.len()
	require.NoError(t, bc.Close())
	requireQuiet(t, rec, n)
}

// Namespaces of one factory share the subscriber, so its loss is one record.
func TestSharedSubscriptionLossLoggedOncePerFactory(t *testing.T) {
	rec := captureRedisLogs(t)
	s := miniredis.RunT(t)
	factory := New(WithAddr(s.Addr()))
	root, err := factory("/")
	require.NoError(t, err)
	chat, err := factory("/chat")
	require.NoError(t, err)

	s.Close()
	require.NoError(t, s.Restart())
	require.Eventually(t, func() bool { return len(rec.records(0, msgSubscriptionRestored)) == 1 },
		5*time.Second, time.Millisecond)
	require.Len(t, rec.records(0, msgSubscriptionLost), 1)

	n := rec.len()
	require.NoError(t, chat.(*broadcast).Close())
	require.NoError(t, root.(*broadcast).Close())
	requireQuiet(t, rec, n)
}

// Closing the adapter ends the subscription on purpose: nothing is logged.
func TestCloseLogsNoSubscriptionLoss(t *testing.T) {
	rec := captureRedisLogs(t)
	s := miniredis.RunT(t)
	bc := newTestRedisBroadcast(t, s)
	require.NoError(t, bc.Close())
	requireQuiet(t, rec, 0)

	// Closing while a reconnect attempt is in flight ends it without a record
	// of the failed attempt.
	blocked := newTestRedisBroadcast(t, s)
	attempt := make(chan struct{}, 1)
	dial := func(ctx context.Context) (redis.Conn, error) {
		select {
		case attempt <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	blocked.dial.Store(&dial)
	blocked.subLock.Lock()
	_ = blocked.sub.Close()
	blocked.subLock.Unlock()
	select {
	case <-attempt:
	case <-time.After(5 * time.Second):
		t.Fatal("no reconnect attempt")
	}
	n := rec.len()
	require.NoError(t, blocked.Close())
	requireQuiet(t, rec, n)

	// Closing during an outage also ends the reconnect attempts quietly.
	other := newTestRedisBroadcast(t, s)
	s.Close()
	require.Eventually(t, func() bool { return len(rec.records(n, msgSubscriptionLost)) == 1 },
		5*time.Second, time.Millisecond)
	n = rec.len()
	require.NoError(t, other.Close())
	requireQuiet(t, rec, n)
}

// reconnectWait is one reconnect delay and the time its wait began.
type reconnectWait struct {
	delay time.Duration
	at    time.Time
}

// recordReconnectWaitTimes records the reconnect delays bc's dispatcher waits.
func recordReconnectWaitTimes(bc *broadcast) func() []reconnectWait {
	var mu sync.Mutex
	var waits []reconnectWait
	wait := func(d time.Duration) <-chan time.Time {
		mu.Lock()
		waits = append(waits, reconnectWait{delay: d, at: time.Now()})
		mu.Unlock()
		return time.After(d)
	}
	bc.reconnectWait.Store(&wait)
	return func() []reconnectWait {
		mu.Lock()
		defer mu.Unlock()
		return append([]reconnectWait(nil), waits...)
	}
}

// recordReconnectWaits records the reconnect delays bc's dispatcher waits.
func recordReconnectWaits(bc *broadcast) func() []time.Duration {
	waits := recordReconnectWaitTimes(bc)
	return func() []time.Duration {
		var out []time.Duration
		for _, w := range waits() {
			out = append(out, w.delay)
		}
		return out
	}
}

// A recovered subscription that stays up for the maximum delay starts the
// backoff over: the first reconnect after a later loss waits the minimum
// delay, not the delay the previous outage reached.
func TestReconnectBackoffRestartsAfterStableSubscription(t *testing.T) {
	rec := captureRedisLogs(t)
	s := miniredis.RunT(t)
	bc := newTestRedisBroadcast(t, s)
	waits := recordReconnectWaits(bc)

	s.Close()
	require.Eventually(t, func() bool {
		w := waits()
		return len(w) > 0 && w[len(w)-1] == redisReconnectMax
	}, 5*time.Second, time.Millisecond, "the first outage must reach the maximum delay")
	require.Equal(t, redisReconnectMin, waits()[0], "the first loss starts at the minimum delay")
	require.NoError(t, s.Restart())
	require.Eventually(t, func() bool { return len(rec.records(0, msgSubscriptionRestored)) == 1 },
		5*time.Second, time.Millisecond)

	// The restored subscription stays up for longer than the maximum delay.
	time.Sleep(2 * redisReconnectMax)
	n := len(waits())
	replaceSubscriber(t, bc)
	w := waits()
	require.Greater(t, len(w), n)
	require.Equal(t, redisReconnectMin, w[n], "a later loss must start the backoff over")
}

// dropAfterSubscribe accepts the subscription and fails the next read, like a
// server that drops every subscriber right after acknowledging it.
type dropAfterSubscribe struct {
	redis.Conn
	acks int
}

func (c *dropAfterSubscribe) Receive() (interface{}, error) {
	if c.acks == 0 {
		return nil, errors.New("dropped after subscribe")
	}
	c.acks--
	return c.Conn.Receive()
}

// A subscription that fails right after each recovery keeps the backoff
// growing instead of reconnecting at the minimum delay: doubling from 5 ms up
// to 50 ms allows about 8 attempts in 300 ms, restarting at 5 ms about 50.
func TestReconnectBackoffGrowsWhenSubscriptionDropsAtOnce(t *testing.T) {
	captureRedisLogs(t)
	s := miniredis.RunT(t)
	bc := newTestRedisBroadcast(t, s)
	var mu sync.Mutex
	var dialed []time.Time
	dialTimes := func() []time.Time {
		mu.Lock()
		defer mu.Unlock()
		return append([]time.Time(nil), dialed...)
	}
	orig := *bc.dial.Load()
	dial := func(ctx context.Context) (redis.Conn, error) {
		mu.Lock()
		dialed = append(dialed, time.Now())
		mu.Unlock()
		c, err := orig(ctx)
		if err != nil {
			return nil, err
		}
		// One acknowledgement for the pattern and one per channel.
		return &dropAfterSubscribe{Conn: c, acks: 3}, nil
	}
	bc.dial.Store(&dial)
	start := time.Now()
	bc.subLock.Lock()
	_ = bc.sub.Close()
	bc.subLock.Unlock()

	require.Eventually(t, func() bool { return len(dialTimes()) >= 2 }, 5*time.Second, time.Millisecond,
		"the subscriber must keep reconnecting")
	// Only the dials of the first 300 ms count, whenever this goroutine wakes.
	time.Sleep(time.Until(start.Add(300 * time.Millisecond)))
	var early int
	for _, at := range dialTimes() {
		if at.Sub(start) <= 300*time.Millisecond {
			early++
		}
	}
	require.LessOrEqual(t, early, 12, "the delay must keep doubling")
}

// A namespace registered or closed on a restored subscription makes Redis
// acknowledge a SUBSCRIBE or UNSUBSCRIBE to the dispatcher. That does not
// start the backoff over: a loss that follows sooner than the maximum delay
// continues it.
func TestReconnectBackoffKeepsGrowingAfterNamespaceChange(t *testing.T) {
	captureRedisLogs(t)
	s := miniredis.RunT(t)
	factory := New(WithAddr(s.Addr()))
	root, err := factory("/")
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.(*broadcast).Close() })
	owner := root.(*broadcast).resources.owner
	waits := recordReconnectWaitTimes(owner)

	// The uptime of the restored subscription is at most the time from the end
	// of the last reconnect delay to the start of the next one. A round in
	// which this goroutine was too slow to lose it within the maximum delay
	// proves nothing and is repeated.
	for round := 0; round < 50; round++ {
		s.Close()
		require.Eventually(t, func() bool {
			w := waits()
			return len(w) > 0 && w[len(w)-1].delay == redisReconnectMax
		}, 5*time.Second, time.Millisecond, "the outage must reach the maximum delay")
		require.NoError(t, s.Restart())
		require.Eventually(t, owner.connected, 5*time.Second, time.Millisecond)

		n := len(waits())
		added, err := factory("/added")
		require.NoError(t, err)
		require.NoError(t, added.(*broadcast).Close())
		replaceSubscriber(t, owner)
		w := waits()
		require.Greater(t, len(w), n)
		if w[n].at.Sub(w[n-1].at.Add(w[n-1].delay)) >= redisReconnectMax {
			continue
		}
		require.Equal(t, redisReconnectMax, w[n].delay,
			"subscription acknowledgements must not start the backoff over")
		return
	}
	t.Skip("the subscription was never lost within the maximum delay of its recovery")
}

// Redis Pub/Sub keeps no messages for a disconnected subscriber: a broadcast
// published while a peer's subscription is down succeeds for the publisher,
// is never delivered to that peer and is not replayed after recovery.
func TestBroadcastDuringSubscriptionGapIsLost(t *testing.T) {
	rec := captureRedisLogs(t)
	s := miniredis.RunT(t)
	a := newTestRedisBroadcast(t, s)
	b := newTestRedisBroadcast(t, s)
	a1 := newRedisTestConn("a1")
	a.Join("room", a1)

	var down atomic.Bool
	down.Store(true)
	orig := *a.dial.Load()
	dial := func(ctx context.Context) (redis.Conn, error) {
		if down.Load() {
			return nil, errors.New("partitioned")
		}
		return orig(ctx)
	}
	a.dial.Store(&dial)
	a.subLock.Lock()
	_ = a.sub.Close()
	a.subLock.Unlock()
	require.Eventually(t, func() bool { return len(rec.records(0, msgSubscriptionLost)) == 1 },
		5*time.Second, time.Millisecond)

	require.NoError(t, b.SendTo(adapter.Options{Rooms: []string{"room"}}, "msg", "in gap"))

	down.Store(false)
	require.Eventually(t, func() bool { return len(rec.records(0, msgSubscriptionRestored)) == 1 },
		5*time.Second, time.Millisecond)
	require.NoError(t, b.SendTo(adapter.Options{Rooms: []string{"room"}}, "msg", "after"))
	expectEvent(t, a1, "msg [after]")
	require.Empty(t, a1.events)
}

// Request helpers return no error, so they log failures: at WARN while the
// adapter is open, at DEBUG once it is closed, like Send and SendAll.
func TestRequestHelperErrorsAfterCloseLogAtDebug(t *testing.T) {
	rec := captureRedisLogs(t)
	s := miniredis.RunT(t)
	bc := newTestRedisBroadcast(t, s)
	open := newTestRedisBroadcast(t, s)
	require.NoError(t, bc.Close())

	require.Nil(t, bc.IDs("room"))
	require.Nil(t, bc.AllRooms())
	require.Equal(t, -1, bc.Len("room"))
	bc.Clear("room")
	bc.SendAll("msg")
	failed := rec.records(0, msgRequestFailed)
	require.Len(t, failed, 4, "one record per failed call")
	requireLevel(t, failed, slog.LevelDebug, true)
	broadcast := rec.records(0, "socketio: redis broadcast failed")
	require.Len(t, broadcast, 1)
	requireLevel(t, broadcast, slog.LevelDebug, true)

	// A failure of answering a peer that ends after Close, such as a reply
	// whose PUBLISH fails, is not ErrAdapterClosed and is still DEBUG.
	n := rec.len()
	bc.logRequestError(errors.New("reply failed"))
	failed = rec.records(n, msgRequestFailed)
	require.Len(t, failed, 1)
	requireLevel(t, failed, slog.LevelDebug, true)

	n = rec.len()
	s.Close()
	require.Equal(t, -1, open.Len("room"))
	failed = rec.records(n, msgRequestFailed)
	require.Len(t, failed, 1)
	requireLevel(t, failed, slog.LevelWarn, true)
}
