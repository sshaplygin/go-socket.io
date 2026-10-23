package client

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// leakedModuleStacks reports goroutines executing library code, including a
// callback reached from the library. Test-only helpers and net/http's own idle
// workers are outside this gate.
func leakedModuleStacks() []string {
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
	var leaks []string
	for _, stack := range strings.Split(string(buffer), "\n\n") {
		for _, line := range strings.Split(stack, "\n") {
			// The final colon precedes the line number (paths can contain a colon).
			file, _, found := strings.Cut(line, ".go:")
			if found && strings.HasPrefix(filepath.ToSlash(file), prefix) && !strings.HasSuffix(file, "_test") {
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
