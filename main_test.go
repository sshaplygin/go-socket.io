package socketio

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// moduleGoroutines returns the stacks, keyed by goroutine ID, of the goroutines
// with a frame in a non-test file of this module, including a callback reached
// from the library. Test-only helpers and net/http's own idle workers are
// outside this set.
func moduleGoroutines() map[string]string {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Dir(source)
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			panic("cannot locate module root for goroutine gate")
		}
		root = parent
	}
	prefix := "\t" + filepath.ToSlash(root) + "/"
	buffer := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buffer, true)
		if n < len(buffer) {
			buffer = buffer[:n]
			break
		}
		buffer = make([]byte, len(buffer)*2)
	}
	goroutines := map[string]string{}
	for _, stack := range strings.Split(string(buffer), "\n\n") {
		for _, line := range strings.Split(stack, "\n") {
			file, _, found := strings.Cut(line, ".go:")
			if found && strings.HasPrefix(filepath.ToSlash(file), prefix) && !strings.HasSuffix(file, "_test") {
				goroutines[strings.Fields(stack)[1]] = stack
				break
			}
		}
	}
	return goroutines
}

// leakedModuleStacks returns the stacks of moduleGoroutines in a stable order.
func leakedModuleStacks() []string {
	goroutines := moduleGoroutines()
	leaks := make([]string, 0, len(goroutines))
	for _, stack := range goroutines {
		leaks = append(leaks, stack)
	}
	sort.Strings(leaks)
	return leaks
}

func TestMain(m *testing.M) {
	code := m.Run()
	deadline := time.Now().Add(5 * time.Second)
	for {
		leaks := leakedModuleStacks()
		if len(leaks) == 0 {
			break
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "library goroutines remain after tests:\n"+strings.Join(leaks, "\n\n"))
			code = 1
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(code)
}
