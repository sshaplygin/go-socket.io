package redis

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/gomodule/redigo/redis"
	"github.com/google/uuid"

	"github.com/sshaplygin/go-socket.io/adapter"
)

// sharedResources owns one subscription loop and publishing pool for a factory.
// owner.subLock orders subscription writes, reconnects, registration and close.
// mu only protects the namespace table; dispatch never holds it while delivering.
type sharedResources struct {
	workers chan struct{}
	owner   *broadcast
	mu      sync.Mutex
	members map[string][]*broadcast
}

func (s *sharedResources) channels() []interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	channels := make([]interface{}, 0, 2*len(s.members))
	for _, group := range s.members {
		bc := group[0]
		channels = append(channels, bc.reqChannel, bc.resChannel)
	}
	return channels
}

func (s *sharedResources) add(nsp string, opts *options) (*broadcast, error) {
	if nsp == "" {
		nsp = "/"
	}
	owner := s.owner
	ctx, cancel := context.WithTimeout(context.Background(), redisDialTimeout)
	defer cancel()
	owner.subLock.Lock()
	select {
	case <-owner.done:
		owner.subLock.Unlock()
		return nil, fmt.Errorf("redis factory resources closed")
	default:
	}
	s.mu.Lock()
	existing := len(s.members[nsp]) > 0
	bc := &broadcast{Memory: adapter.NewMemory(), requests: make(map[string]interface{}), done: make(chan struct{}), pub: owner.pub, resources: s, nsp: nsp, uid: uuid.NewString(),
		key: fmt.Sprintf("%s#%s#", opts.Prefix, nsp), reqChannel: fmt.Sprintf("%s-request#%s#", opts.Prefix, nsp), resChannel: fmt.Sprintf("%s-response#%s#", opts.Prefix, nsp)}
	bc.initCluster(opts)
	s.members[nsp] = append(s.members[nsp], bc)
	s.mu.Unlock()
	var err error
	stop := func() bool { return true }
	if !existing && owner.connected() {
		sub := owner.sub
		// Closing the captured generation bounds both command flush and ACK wait.
		// Redis connections permit Close concurrently with their reader/writer.
		stop = context.AfterFunc(ctx, func() { _ = sub.Close() })
		err = sub.Subscribe(bc.reqChannel, bc.resChannel)
	}
	defer stop()
	owner.subLock.Unlock()
	if err == nil {
		err = bc.waitReady(ctx)
	}
	if !stop() && err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = bc.Close()
		return nil, err
	}
	return bc, nil
}

func (s *sharedResources) dispatch(m redis.Message) {
	s.mu.Lock()
	groups := make([][]*broadcast, 0, len(s.members))
	for _, group := range s.members {
		active := make([]*broadcast, 0, len(group))
		for _, bc := range group {
			if !bc.closed.Load() {
				active = append(active, bc)
			}
		}
		if len(active) != 0 {
			groups = append(groups, active)
		}
	}
	s.mu.Unlock()
	for _, group := range groups {
		if m.Channel == group[0].reqChannel {
			s.onRequest(group, m.Data)
			continue
		}
		for _, bc := range group {
			bc.handleMessage(m)
		}
	}
}

// Redis counts one subscriber per shared connection. Answer once with the
// combined local state when a factory has multiple adapters for a namespace.
func (s *sharedResources) onRequest(group []*broadcast, data []byte) {
	if len(group) > 0 {
		group[0].handleClusterRequest(group, data)
	}
}

func (s *sharedResources) clusterWorkers() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workers == nil {
		s.workers = make(chan struct{}, 32)
	}
	return s.workers
}

func (s *sharedResources) remove(bc *broadcast) error {
	owner := s.owner
	owner.subLock.Lock()
	s.mu.Lock()
	group := s.members[bc.nsp]
	found := -1
	for i, member := range group {
		if member == bc {
			found = i
			break
		}
	}
	if found < 0 {
		s.mu.Unlock()
		owner.subLock.Unlock()
		return nil
	}
	group = append(group[:found], group[found+1:]...)
	if len(group) == 0 {
		delete(s.members, bc.nsp)
	} else {
		s.members[bc.nsp] = group
	}
	empty := len(s.members) == 0
	s.mu.Unlock()
	if empty {
		err := owner.closeTransport()
		owner.subLock.Unlock()
		return err
	}
	if len(group) > 0 {
		owner.subLock.Unlock()
		return nil
	}
	owner.subscriptionChanged(bc.reqChannel, false)
	owner.subscriptionChanged(bc.resChannel, false)
	sub := owner.sub
	ack := owner.beginUnsubscribe(sub, bc.reqChannel, bc.resChannel)
	ctx, cancel := context.WithTimeout(context.Background(), redisDialTimeout)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = sub.Close() })
	defer stop()
	err := sub.Unsubscribe(bc.reqChannel, bc.resChannel)
	owner.subLock.Unlock()
	defer owner.endUnsubscribe(ack)
	if err != nil {
		// The subscriber can fail between readiness and this write. Its
		// replacement excludes the removed member and confirms the unsubscribe.
		_ = sub.Close()
	}
	select {
	case <-ack.done:
		return nil
	case <-owner.done:
		return nil
	case <-ctx.Done():
		return errors.Join(ctx.Err(), err)
	}
}
