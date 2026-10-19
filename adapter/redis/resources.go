package redis

import (
	"encoding/json"
	"fmt"
	"github.com/gomodule/redigo/redis"
	"github.com/google/uuid"
	"github.com/sshaplygin/go-socket.io/adapter"
	"sync"
)

// sharedResources owns one subscription loop and publishing pool for a factory.
// owner.subLock orders subscription writes, reconnects, registration and close.
// mu only protects the namespace table; dispatch never holds it while delivering.
type sharedResources struct {
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
	owner := s.owner
	owner.subLock.Lock()
	defer owner.subLock.Unlock()
	select {
	case <-owner.done:
		return nil, fmt.Errorf("redis factory resources closed")
	default:
	}
	s.mu.Lock()
	existing := len(s.members[nsp]) > 0
	uid := uuid.NewString()
	bc := &broadcast{Memory: adapter.NewMemory(), requests: make(map[string]interface{}), done: make(chan struct{}), pub: owner.pub, resources: s, nsp: nsp, uid: uid,
		key: fmt.Sprintf("%s#%s#%s", opts.Prefix, nsp, uid), reqChannel: fmt.Sprintf("%s-request#%s", opts.Prefix, nsp), resChannel: fmt.Sprintf("%s-response#%s", opts.Prefix, nsp)}
	s.members[nsp] = append(s.members[nsp], bc)
	s.mu.Unlock()
	if !existing {
		if err := owner.sub.Subscribe(bc.reqChannel, bc.resChannel); err != nil {
			s.mu.Lock()
			delete(s.members, nsp)
			s.mu.Unlock()
			return nil, err
		}
	}
	return bc, nil
}

func (s *sharedResources) dispatch(m redis.Message) {
	s.mu.Lock()
	groups := make([][]*broadcast, 0, len(s.members))
	for _, group := range s.members {
		groups = append(groups, append([]*broadcast(nil), group...))
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
	var req map[string]string
	if json.Unmarshal(data, &req) != nil {
		return
	}
	bc := group[0]
	switch req["RequestType"] {
	case roomLenReqType:
		count := 0
		for _, member := range group {
			count += member.Memory.Len(req["Room"])
		}
		bc.publish(bc.resChannel, &roomLenResponse{RequestType: roomLenReqType, RequestID: req["RequestID"], Connections: count})
	case allRoomReqType:
		seen := map[string]bool{}
		rooms := []string{}
		for _, member := range group {
			for _, room := range member.Memory.AllRooms() {
				if !seen[room] {
					seen[room] = true
					rooms = append(rooms, room)
				}
			}
		}
		bc.publish(bc.resChannel, &allRoomResponse{RequestType: allRoomReqType, RequestID: req["RequestID"], Rooms: rooms})
	case clearRoomReqType:
		for _, member := range group {
			if member.uid != req["UUID"] {
				member.Memory.Clear(req["Room"])
			}
		}
	}
}

func (s *sharedResources) remove(bc *broadcast) error {
	owner := s.owner
	owner.subLock.Lock()
	defer owner.subLock.Unlock()
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
		return owner.closeTransport()
	}
	if len(group) == 0 {
		return owner.sub.Unsubscribe(bc.reqChannel, bc.resChannel)
	}
	return nil
}
