package polling

import "time"

// batchClock is optional and installed before a payload is used. A nil clock
// preserves the transport's real-time batching policy without allocating a clock.
type batchClock struct {
	now      func() time.Time
	newTimer func(time.Duration) batchTimer
}

// batchTimer keeps the production timer concrete; only injected clocks use stop.
// Returning this value does not allocate another wrapper around time.Timer.
type batchTimer struct {
	C    <-chan time.Time
	real *time.Timer
	stop func()
}

func (t batchTimer) Stop() {
	if t.real != nil {
		t.real.Stop()
	} else {
		t.stop()
	}
}

func (p *payload) batchNow() time.Time {
	if p.batchClock != nil {
		return p.batchClock.now()
	}
	return time.Now()
}

func (p *payload) batchUntil(t time.Time) time.Duration {
	if p.batchClock != nil {
		return t.Sub(p.batchClock.now())
	}
	return time.Until(t)
}

func (p *payload) newBatchTimer(d time.Duration) batchTimer {
	if p.batchClock != nil {
		return p.batchClock.newTimer(d)
	}
	t := time.NewTimer(d)
	return batchTimer{C: t.C, real: t}
}
