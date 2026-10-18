package session

import (
	"sync"
)

// Manager owns the server session registry and identifier provider.
type Manager struct {
	IDGenerator

	sessions map[string]*Session
	locker   sync.RWMutex
}

// NewManager creates an empty registry; nil selects cryptographic random IDs.
func NewManager(gen IDGenerator) *Manager {
	if gen == nil {
		gen = &DefaultIDGenerator{}
	}
	return &Manager{
		IDGenerator: gen,
		sessions:    make(map[string]*Session),
	}
}

func (m *Manager) Add(s *Session) {
	m.locker.Lock()
	defer m.locker.Unlock()

	m.sessions[s.ID()] = s
}

func (m *Manager) Get(sid string) (*Session, bool) {
	m.locker.RLock()
	defer m.locker.RUnlock()

	s, ok := m.sessions[sid]
	return s, ok
}

func (m *Manager) Remove(sid string) {
	m.locker.Lock()
	defer m.locker.Unlock()

	if _, ok := m.sessions[sid]; !ok {
		return
	}
	delete(m.sessions, sid)
}

func (m *Manager) Count() int {
	m.locker.RLock()
	defer m.locker.RUnlock()

	return len(m.sessions)
}

// All returns a snapshot of the registered sessions keyed by session ID.
func (m *Manager) All() map[string]*Session {
	m.locker.RLock()
	defer m.locker.RUnlock()
	sessions := make(map[string]*Session, len(m.sessions))
	for id, s := range m.sessions {
		sessions[id] = s
	}
	return sessions
}
