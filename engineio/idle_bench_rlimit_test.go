//go:build linux || darwin

package engineio_test

import (
	"fmt"
	"syscall"
)

const idleBenchSupported = true

// raiseNoFile raises the soft RLIMIT_NOFILE of this process to at least need. The server
// subprocess inherits the limit. Rlimit fields are uint64 on linux and darwin only. It returns an error when the hard limit is too low.
func raiseNoFile(need uint64) error {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return err
	}
	if lim.Cur >= need {
		return nil
	}
	want := lim
	want.Cur = need
	if want.Cur > lim.Max {
		want.Max = want.Cur
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &want); err != nil {
		// Some systems (macOS) refuse a value above their per-process kernel cap even
		// when the hard limit says unlimited, and refuse raising the hard limit
		// without privileges. Retry within the existing hard limit.
		want = lim
		want.Cur = lim.Max
		if err2 := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &want); err2 != nil {
			return fmt.Errorf("setrlimit NOFILE %d: %w", need, err)
		}
	}
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return err
	}
	if lim.Cur < need {
		return fmt.Errorf("RLIMIT_NOFILE soft limit is %d, need %d", lim.Cur, need)
	}
	return nil
}
