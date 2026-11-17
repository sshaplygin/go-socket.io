package redis

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
	if !strings.Contains(string(buffer), prefix) {
		// This goroutine runs this file, so a dump that never names the module
		// root cannot attribute any goroutine, for example under -trimpath.
		panic("goroutine gate cannot match stack paths to " + root + "; is -trimpath set?")
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

// moduleGoroutineGrace is how long library goroutines may outlive the Close
// calls of the cleanups that ended them.
const moduleGoroutineGrace = 5 * time.Second

// awaitModuleGoroutines waits until no goroutine of moduleGoroutines remains
// and returns the stacks of those still running after moduleGoroutineGrace.
func awaitModuleGoroutines() []string {
	deadline := time.Now().Add(moduleGoroutineGrace)
	for {
		leaks := leakedModuleStacks()
		if len(leaks) == 0 || time.Now().After(deadline) {
			return leaks
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	if leaks := awaitModuleGoroutines(); len(leaks) > 0 {
		fmt.Fprintln(os.Stderr, "library goroutines remain after tests:\n"+strings.Join(leaks, "\n\n"))
		code = 1
	}
	os.Exit(code)
}
