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
