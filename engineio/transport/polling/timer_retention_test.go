package polling

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPollingTimersReleasedUnderAsyncTimerChan(t *testing.T) {
	if os.Getenv("SOCKETIO_TIMER_RETENTION_CHILD") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestPollingTimersReleasedUnderAsyncTimerChan$", "-test.count=1")
		command.Env = append(os.Environ(), "GODEBUG=asynctimerchan=1", "SOCKETIO_TIMER_RETENTION_CHILD=1", "GOMAXPROCS=2")
		output, err := command.CombinedOutput()
		require.NoError(t, err, "timer retention child: %s", output)
		t.Log(string(output))
		return
	}
	p := newPayload(false)
	defer func() { _ = p.Close() }()
	deadline := time.Now().Add(45 * time.Second)
	require.NoError(t, p.SetReadDeadline(deadline))
	require.NoError(t, p.SetWriteDeadline(deadline))
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fed := make(chan error, 1)
	for i := 0; i < 50000; i++ {
		// Renew the same 45-second liveness window as packet traffic does.
		// A loaded runner must not fail on an unrelated total-workload deadline.
		require.NoError(t, p.SetReadDeadline(time.Now().Add(45*time.Second)))
		go func() { fed <- p.FeedIn(strings.NewReader("6:4hello"), false) }()
		_, _, r, err := p.NextReader()
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		require.NoError(t, <-fed)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	growth := int64(after.HeapInuse) - int64(before.HeapInuse)
	fmt.Printf("retained heap growth: %d bytes\n", growth)
	require.Less(t, growth, int64(8<<20))
}
