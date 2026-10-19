package redis

import (
	"context"
	"sync"

	"github.com/gomodule/redigo/redis"
)

// subscriptionState belongs to the shared transport, including when its original
// namespace adapter has closed. All subscribers await the same generation.
type subscriptionState struct {
	mu           sync.Mutex
	changed      chan struct{}
	channels     map[string]bool
	unsubscribes map[*unsubscribeAck]struct{}
	queues       map[string][]*unsubscribeAck
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
		if err := ctx.Err(); err != nil {
			return err
		}
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
	bc.confirmReplacement()
}

// unsubscribeAck belongs to one captured subscriber generation. A replacement
// generation also confirms removal after registering only current members.
type unsubscribeAck struct {
	sub      *redis.PubSubConn
	channels map[string]bool
	done     chan struct{}
}

func (bc *broadcast) beginUnsubscribe(sub *redis.PubSubConn, channels ...string) *unsubscribeAck {
	s := &bc.transport().readiness
	s.mu.Lock()
	defer s.mu.Unlock()
	ack := &unsubscribeAck{sub: sub, channels: make(map[string]bool), done: make(chan struct{})}
	for _, channel := range channels {
		ack.channels[channel] = true
		if s.queues == nil {
			s.queues = make(map[string][]*unsubscribeAck)
		}
		s.queues[channel] = append(s.queues[channel], ack)
	}
	if s.unsubscribes == nil {
		s.unsubscribes = make(map[*unsubscribeAck]struct{})
	}
	s.unsubscribes[ack] = struct{}{}
	return ack
}
func (bc *broadcast) endUnsubscribe(ack *unsubscribeAck) {
	s := &bc.transport().readiness
	s.mu.Lock()
	defer s.mu.Unlock()
	// Keep its queue positions until the ACK or generation replacement; a late
	// ACK must not confirm a later unsubscribe for the same namespace.
	delete(s.unsubscribes, ack)
}
func (bc *broadcast) acknowledgeUnsubscribe(sub *redis.PubSubConn, channel string) {
	s := &bc.transport().readiness
	s.mu.Lock()
	defer s.mu.Unlock()
	queue := s.queues[channel]
	for i, ack := range queue {
		if ack.sub != sub {
			continue
		}
		queue = append(queue[:i], queue[i+1:]...)
		if len(queue) == 0 {
			delete(s.queues, channel)
		} else {
			s.queues[channel] = queue
		}
		delete(ack.channels, channel)
		if len(ack.channels) == 0 {
			if _, waiting := s.unsubscribes[ack]; waiting {
				close(ack.done)
				delete(s.unsubscribes, ack)
			}
		}
		return
	}
}
func (bc *broadcast) confirmReplacement() {
	s := &bc.transport().readiness
	s.mu.Lock()
	defer s.mu.Unlock()
	for channel, queue := range s.queues {
		active := queue[:0]
		for _, ack := range queue {
			if ack.sub == bc.sub {
				active = append(active, ack)
			}
		}
		if len(active) == 0 {
			delete(s.queues, channel)
		} else {
			s.queues[channel] = active
		}
	}
	for ack := range s.unsubscribes {
		if ack.sub != bc.sub {
			close(ack.done)
			delete(s.unsubscribes, ack)
		}
	}
}
