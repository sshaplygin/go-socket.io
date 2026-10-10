package session

import (
	"testing"
	"time"
)

// TestManagerCountAlongsideReaders checks that Count is a read operation:
// it must not wait for other readers of the session map.
func TestManagerCountAlongsideReaders(t *testing.T) {
	m := NewManager(nil)
	m.locker.RLock()
	defer m.locker.RUnlock()

	done := make(chan int, 1)
	go func() { done <- m.Count() }()

	select {
	case n := <-done:
		if n != 0 {
			t.Fatalf("Count() = %d, want 0", n)
		}
	case <-time.After(time.Second):
		t.Fatal("Count blocked behind a concurrent reader")
	}
}

func TestManagerAddGetRemove(t *testing.T) {
	m := NewManager(nil)
	sid := m.NewID()
	if sid == "" || sid == m.NewID() {
		t.Fatal("NewID must return distinct non-empty ids")
	}

	s := &Session{params: transportParams(sid)}
	m.Add(s)
	if got, ok := m.Get(sid); !ok || got != s {
		t.Fatalf("Get(%q) = %v, %v; want the added session", sid, got, ok)
	}
	if m.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", m.Count())
	}

	m.Remove(sid)
	m.Remove(sid) // removing twice is a no-op
	if _, ok := m.Get(sid); ok || m.Count() != 0 {
		t.Fatal("session still present after Remove")
	}
}
