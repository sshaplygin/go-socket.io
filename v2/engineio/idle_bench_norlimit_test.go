//go:build !(linux || darwin)

package engineio_test

// The idle-connection benchmark needs fork/exec of the test binary, ps and
// RLIMIT_NOFILE with uint64 fields; it skips itself outside linux and darwin.
const idleBenchSupported = false

func raiseNoFile(uint64) error { return nil }
