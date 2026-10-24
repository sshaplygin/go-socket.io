package session

import (
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

type blockingDeadlineConn struct {
	scriptConn
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (c *blockingDeadlineConn) SetWriteDeadline(time.Time) error {
	if c.armed.Load() {
		close(c.entered)
		<-c.release
	}
	return nil
}

func TestSetDeadlineDoesNotBlockClose(t *testing.T) {
	c := &blockingDeadlineConn{
		scriptConn: scriptConn{reads: []scriptedPacket{{ft: eioparser.String, pt: eioparser.PING}}},
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	s := newTestSession(t, c, "websocket")
	release := sync.OnceFunc(func() { close(c.release) })
	t.Cleanup(release)
	c.armed.Store(true)
	readDone := make(chan struct{})
	go func() { _, _, _ = s.NextReader(); close(readDone) }()
	select {
	case <-c.entered:
	case <-time.After(time.Second):
		t.Fatal("reader did not enter deadline update")
	}
	closed := make(chan error, 1)
	go func() { closed <- s.Abort() }()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Abort blocked behind the transport deadline update")
	}
	release()
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("reader did not exit after Abort")
	}
}

func TestFailDoesNotWaitForPendingClose(t *testing.T) {
	s := newTestSession(t, &scriptConn{}, "websocket")
	s.upgradeLocker.RLock()
	unlock := sync.OnceFunc(s.upgradeLocker.RUnlock)
	t.Cleanup(unlock)
	closed := make(chan error, 1)
	go func() { closed <- s.Abort() }()
	deadline := time.Now().Add(time.Second)
	for s.upgradeLocker.TryRLock() {
		s.upgradeLocker.RUnlock()
		if time.Now().After(deadline) {
			t.Fatal("Abort did not request the session lock")
		}
		runtime.Gosched()
	}
	failed := make(chan error, 1)
	go func() { failed <- s.fail(io.ErrClosedPipe) }()
	select {
	case err := <-failed:
		require.ErrorIs(t, err, io.ErrClosedPipe)
	case <-time.After(time.Second):
		t.Fatal("fail blocked behind a pending close")
	}
	unlock()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Abort did not finish after releasing the lock")
	}
}
