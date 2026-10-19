package redis

import (
	"context"
	"sync"
)

// subscriptionState belongs to the shared transport, including when its original
// namespace adapter has closed. All subscribers await the same generation.
type subscriptionState struct {
	mu       sync.Mutex
	changed  chan struct{}
	channels map[string]bool
}

func (bc *broadcast) transport() *broadcast {
	if bc.resources != nil {
		return bc.resources.owner
	}
	return bc
}
func (bc *broadcast) subscriptionChanged(channel string, active bool) {
	s := &bc.transport().readiness
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.changed == nil {
		s.changed = make(chan struct{})
	}
	if s.channels == nil {
		s.channels = make(map[string]bool)
	}
	if channel == "" {
		clear(s.channels)
	} else if active {
		s.channels[channel] = true
	} else {
		delete(s.channels, channel)
	}
	close(s.changed)
	s.changed = make(chan struct{})
}
func (bc *broadcast) waitReady(ctx context.Context) error {
	owner := bc.transport()
	s := &owner.readiness
	for {
		if bc.closed.Load() {
			return errAdapterClosed
		}
		s.mu.Lock()
		ready := s.channels[owner.pattern] && s.channels[bc.reqChannel] && s.channels[bc.resChannel]
		if s.changed == nil {
			s.changed = make(chan struct{})
		}
		changed := s.changed
		s.mu.Unlock()
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-owner.done:
			return errAdapterClosed
		case <-changed:
		}
	}
}

func (bc *broadcast) notifyReadiness() {
	s := &bc.transport().readiness
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.changed != nil {
		close(s.changed)
	}
	s.changed = make(chan struct{})
}
func (bc *broadcast) connected() bool {
	owner := bc.transport()
	s := &owner.readiness
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.channels[owner.pattern]
}
func sameChannels(a, b []interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[interface{}]bool, len(a))
	for _, v := range a {
		set[v] = true
	}
	for _, v := range b {
		if !set[v] {
			return false
		}
	}
	return true
}

func (bc *broadcast) markReady(channels []interface{}) {
	bc.subscriptionChanged(bc.pattern, true)
	for _, channel := range channels {
		bc.subscriptionChanged(channel.(string), true)
	}
}
