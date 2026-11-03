package polling

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServerPollDoneAllocatedOnlyForDrainedWaiters(t *testing.T) {
	c := &serverConn{}
	require.Equal(t, (<-chan struct{})(closedChan), c.Drained())
	require.True(t, c.beginPoll())
	require.Nil(t, c.pollDone, "ordinary GETs should not allocate a drain channel")
	c.endPoll()
	require.Nil(t, c.pollDone)
	require.Equal(t, (<-chan struct{})(closedChan), c.Drained())

	allocs := testing.AllocsPerRun(100, func() {
		if !c.beginPoll() {
			panic("poll still active")
		}
		c.endPoll()
	})
	require.Zero(t, allocs, "a GET with no drain waiter should not allocate a channel")
}

func TestServerPollDoneClosesAllWaitersAndStartsFresh(t *testing.T) {
	c := &serverConn{}
	require.True(t, c.beginPoll())
	const waiters = 16
	observed := make(chan (<-chan struct{}), waiters)
	finished := make(chan struct{}, waiters)
	var wg sync.WaitGroup
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := c.Drained()
			observed <- ch
			<-ch
			finished <- struct{}{}
		}()
	}
	first := <-observed
	for i := 1; i < waiters; i++ {
		require.Equal(t, first, <-observed, "all waiters for one GET must share its completion")
	}
	select {
	case <-first:
		t.Fatal("active poll reported drained")
	default:
	}
	require.False(t, c.beginPoll(), "overlapping GET must still be refused")
	c.endPoll()
	wg.Wait()
	require.Len(t, finished, waiters)
	require.Nil(t, c.pollDone)
	require.Equal(t, (<-chan struct{})(closedChan), c.Drained())

	require.True(t, c.beginPoll())
	second := c.Drained()
	require.NotEqual(t, first, second, "a later GET needs its own completion channel")
	c.endPoll()
	select {
	case <-second:
	default:
		t.Fatal("later GET waiter was not released")
	}
}
