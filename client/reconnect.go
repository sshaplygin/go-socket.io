package client

import (
	"math"
	"math/rand"
	"time"
)

type reconnectRun struct{ stop, done chan struct{} }

// m.mu is held by the caller. Removing the token also fences a dial that
// returns after Close or a new explicit connection lifecycle.
func (m *clientState) cancelRetryLocked() {
	if m.retry != nil {
		close(m.retry.stop)
		m.retry = nil
	}
}

func (m *clientState) retryCurrent(run *reconnectRun) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.retry == run && !m.stopped
}

func (m *clientState) connectionEnded(previous *conn) {
	<-previous.readStopped
	<-previous.writeStopped
	<-previous.errorStopped
	m.mu.Lock()
	if m.conn != previous || m.stopped || m.attempt != nil || m.retry != nil || m.client.options.DisableReconnection {
		m.mu.Unlock()
		return
	}
	active := false
	for _, s := range m.order {
		active = active || s.active
	}
	if !active {
		m.mu.Unlock()
		return
	}
	run := &reconnectRun{stop: make(chan struct{}), done: make(chan struct{})}
	m.retry = run
	m.mu.Unlock()
	m.reconnect(run)
}

// This goroutine owns all reconnect notifications and starts only after the old
// connection's handlers and workers have returned.
func (m *clientState) reconnect(run *reconnectRun) {
	c := m.client
	defer func() {
		m.mu.Lock()
		if m.retry == run {
			m.cancelRetryLocked()
		}
		m.mu.Unlock()
		close(run.done)
	}()
	for n := 1; m.retryCurrent(run); n++ {
		timer := time.NewTimer(reconnectDelay(c.options, n, rand.Float64()))
		select {
		case <-run.stop:
			timer.Stop()
			return
		case <-timer.C:
		}
		if !m.retryCurrent(run) {
			return
		}
		if f := c.observerSnapshot().attempt; f != nil {
			f(n)
		}
		if !m.retryCurrent(run) {
			return
		}
		err := m.open(c, run, n)
		if err == nil || !m.retryCurrent(run) {
			return
		}
		if f := c.observerSnapshot().err; f != nil {
			f(err)
		}
		if !m.retryCurrent(run) {
			return
		}
		if c.options.ReconnectionAttempts > 0 && n >= c.options.ReconnectionAttempts {
			m.mu.Lock()
			if m.retry != run {
				m.mu.Unlock()
				return
			}
			m.cancelRetryLocked()
			m.mu.Unlock()
			if f := c.observerSnapshot().failed; f != nil {
				f()
			}
			return
		}
	}
}

// backo2 uses one sample for both the deviation and its sign, then caps it.
// Floating point growth avoids duration overflow on an unlimited retry loop.
func reconnectDelay(opts Options, attempt int, sample float64) time.Duration {
	delay := float64(opts.ReconnectionDelay) * math.Pow(2, float64(attempt-1))
	factor := opts.RandomizationFactor
	if factor > 0 && factor <= 1 {
		deviation := math.Floor(sample * factor * delay)
		if int(sample*10)&1 == 0 {
			delay -= deviation
		} else {
			delay += deviation
		}
	}
	if math.IsNaN(delay) || delay >= float64(opts.ReconnectionDelayMax) {
		return opts.ReconnectionDelayMax
	}
	if delay <= 0 {
		return 0
	}
	return time.Duration(delay)
}
