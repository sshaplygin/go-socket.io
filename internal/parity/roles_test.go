package parity

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoleConsumerFixtures(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		name    string
		failure string
	}{
		{"positive", ""},
		{"wrong_server_callback", "cannot use"},
		{"wrong_client_callback", "cannot use"},
		{"retired_root_client", "undefined: socketio.NewClient"},
		{"retired_client_constructor", "undefined: client.NewClient"},
		{"retired_engine_dialer", "undefined: engineio.Dialer"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source := filepath.Join(root, "internal", "parity", "testdata", "roles", fixture.name+".go")
			cmd := exec.Command("go", "test", "-mod=readonly", source)
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
			out, err := cmd.CombinedOutput()
			if fixture.failure == "" {
				if err != nil {
					t.Fatalf("consumer does not compile: %v\n%s", err, out)
				}
				return
			}
			if err == nil || !strings.Contains(string(out), fixture.failure) {
				t.Fatalf("expected %q compilation error, got %v\n%s", fixture.failure, err, out)
			}
		})
	}
}
