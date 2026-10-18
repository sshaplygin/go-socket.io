package engineio

import (
	"sync"
)

type sessionManager struct {
	IDGenerator

	sessions map[string]*session
	locker   sync.RWMutex
}

func newSessionManager(gen IDGenerator) *sessionManager {
	if gen == nil {
		gen = &DefaultIDGenerator{}
	}
	return &sessionManager{
		IDGenerator: gen,
		sessions:    make(map[string]*session),
	}
}

func (m *sessionManager) Add(s *session) {
	m.locker.Lock()
	defer m.locker.Unlock()

	m.sessions[s.ID()] = s
}

func (m *sessionManager) Get(sid string) (*session, bool) {
	m.locker.RLock()
	defer m.locker.RUnlock()

	s, ok := m.sessions[sid]
	return s, ok
}

func (m *sessionManager) Remove(sid string) {
	m.locker.Lock()
	defer m.locker.Unlock()

	if _, ok := m.sessions[sid]; !ok {
		return
	}
	delete(m.sessions, sid)
}

func (m *sessionManager) Count() int {
	m.locker.RLock()
	defer m.locker.RUnlock()

	return len(m.sessions)
}

// All returns a snapshot of the registered sessions keyed by session ID.
func (m *sessionManager) All() map[string]*session {
	m.locker.RLock()
	defer m.locker.RUnlock()
	sessions := make(map[string]*session, len(m.sessions))
	for id, s := range m.sessions {
		sessions[id] = s
	}
	return sessions
}
