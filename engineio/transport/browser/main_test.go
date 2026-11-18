//go:build js && wasm

package browser

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// libraryGoroutines returns the stacks of the goroutines, other than the caller's,
// with a frame whose function belongs to this module and whose file is not a test
// file. It matches function names, so that it does not depend on the file system,
// which wasmbrowsertest reaches through the host.
func libraryGoroutines() []string {
	buffer := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buffer, true)
		if n < len(buffer) {
			buffer = buffer[:n]
			break
		}
		buffer = make([]byte, 2*len(buffer))
	}
	var leaks []string
	// The first stack is the caller's; frames are a function line and a file line.
	for _, stack := range strings.Split(string(buffer), "\n\n")[1:] {
		lines := strings.Split(stack, "\n")
		for i := 1; i+1 < len(lines); i += 2 {
			file, _, _ := strings.Cut(lines[i+1], ".go:")
			if strings.HasPrefix(lines[i], "github.com/sshaplygin/go-socket.io/") && !strings.HasSuffix(file, "_test") {
				leaks = append(leaks, stack)
				break
			}
		}
	}
	return leaks
}

func TestMain(m *testing.M) {
	code := m.Run()
	deadline := time.Now().Add(5 * time.Second)
	for leaks := libraryGoroutines(); len(leaks) != 0; leaks = libraryGoroutines() {
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "library goroutines remain after tests:\n"+strings.Join(leaks, "\n\n"))
			code = 1
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(code)
}
