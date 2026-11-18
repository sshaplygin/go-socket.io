//go:build js && wasm

// Package browsertest holds the browser tests of the client: js/wasm tests that make
// test-browser runs in headless Chrome against the fixture server of ./server.
package browsertest

import (
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"syscall/js"
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

// TestMain requires the fixture server on another origin of the same host as the
// page, so that every test is cross-origin and same-site, and fails when library
// goroutines remain after the tests.
func TestMain(m *testing.M) {
	server, err := url.Parse(os.Getenv("SOCKETIO_BROWSER_URL"))
	page := js.Global().Get("location")
	switch {
	case err != nil || server.Host == "":
		fmt.Fprintln(os.Stderr, "SOCKETIO_BROWSER_URL is not set: run make test-browser")
		os.Exit(1)
	case page.Get("origin").String() == server.Scheme+"://"+server.Host || page.Get("hostname").String() != server.Hostname():
		fmt.Fprintf(os.Stderr, "page %s and server %s must be different origins of one host\n", page.Get("origin"), server)
		os.Exit(1)
	}
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
