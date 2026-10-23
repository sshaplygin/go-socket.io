package polling

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/stretchr/testify/require"
)

type manualBatchTimer struct {
	at time.Time
	ch chan time.Time
}

type manualBatchClock struct {
	mu      sync.Mutex
	now     time.Time
	timers  map[*manualBatchTimer]bool
	created chan time.Duration
}

func newManualBatchClock(p *payload) *manualBatchClock {
	c := &manualBatchClock{
		now: time.Unix(1, 0), timers: make(map[*manualBatchTimer]bool),
		created: make(chan time.Duration, 128),
	}
	p.batchClock = &batchClock{now: c.current, newTimer: c.newTimer}
	return c
}

func (c *manualBatchClock) current() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualBatchClock) newTimer(d time.Duration) batchTimer {
	c.mu.Lock()
	t := &manualBatchTimer{at: c.now.Add(d), ch: make(chan time.Time, 1)}
	c.timers[t] = true
	c.mu.Unlock()
	c.created <- d
	return batchTimer{C: t.ch, stop: func() {
		c.mu.Lock()
		delete(c.timers, t)
		c.mu.Unlock()
	}}
}

func (c *manualBatchClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	for t := range c.timers {
		if !t.at.After(c.now) {
			t.ch <- c.now
			delete(c.timers, t)
		}
	}
}

func (c *manualBatchClock) waitTimers(t *testing.T, n int) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for i := 0; i < n; i++ {
		select {
		case duration := <-c.created:
			require.Equal(t, time.Millisecond, duration, "batch idle window changed")
		case <-deadline.C:
			t.Fatal("batch did not reach its next idle wait")
		}
	}
}

func serveBatchUntilIdle(t *testing.T, c *serverConn, req *http.Request, clock *manualBatchClock, packets int) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); c.ServeHTTP(response, req) }()
	clock.waitTimers(t, packets)
	clock.advance(time.Millisecond)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("batch did not finish after its idle window")
	}
	return response
}

func TestPollingBatchStopsAtTotalWindow(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://localhost/?b64=1", nil)
	c := newServerConn(Default, req)
	defer func() { _ = c.Close() }()
	clock := newManualBatchClock(c.payload)
	written := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			w, err := c.NextWriter(eioparser.String, eioparser.MESSAGE)
			if err != nil {
				written <- err
				return
			}
			if i == 1 {
				// The first packet established the total deadline; this second
				// packet already owns the batch writer, so idle cannot split it.
				clock.advance(5 * time.Millisecond)
			}
			_, err = io.WriteString(w, "a")
			closeErr := w.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				written <- err
				return
			}
		}
		written <- nil
	}()
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); c.ServeHTTP(response, req) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("batch exceeded its five-millisecond total window")
	}
	require.NoError(t, <-written)
	require.Equal(t, "2:4a2:4a", response.Body.String())
	clock.waitTimers(t, 1)
	select {
	case <-clock.created:
		t.Fatal("batch started another idle wait after its total window")
	default:
	}
}

func BenchmarkBatchTimer(b *testing.B) {
	b.Run("direct", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			t := time.NewTimer(time.Hour)
			t.Stop()
		}
	})
	b.Run("production-clock", func(b *testing.B) {
		b.ReportAllocs()
		var p payload
		for i := 0; i < b.N; i++ {
			t := p.newBatchTimer(time.Hour)
			t.Stop()
		}
	})
}
