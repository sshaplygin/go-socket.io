package engineio

import (
	"encoding/base64"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Covers E15
func TestRandomSessionID(t *testing.T) {
	g := &DefaultIDGenerator{}
	ids := make(chan string, 1000)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ids <- g.NewID()
			}
		}()
	}
	wg.Wait()
	close(ids)
	seen := make(map[string]bool)
	for id := range ids {
		b, err := base64.RawURLEncoding.DecodeString(id)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(b), 15, "session ids need at least 120 random bits")
		require.False(t, seen[id], "duplicate session id")
		seen[id] = true
	}
}
