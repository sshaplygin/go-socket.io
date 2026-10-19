package parity

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
		{"adapter", ""},
		{"engine_session", ""},
		{"engine_codecs", ""},
		{"external_server", ""},
		{"external_client", ""},
		{"external_client_namespace", "undefined: client.NamespaceHandler"},
		{"external_client_connection", "undefined: client.Manager"},
		{"external_server_namespace", "undefined: socketio.NamespaceHandler"},
		{"external_server_connection", "undefined: socketio.Connection"},
		{"private_packet_codec", "use of internal package"},
		{"private_payload_codec", "use of internal package"},
		{"private_engine_session", "use of internal package"},
		{"wrong_server_callback", "cannot use"},
		{"wrong_client_callback", "cannot use"},
		{"wrong_server_option", "cannot use"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source := filepath.Join(root, "internal", "parity", "testdata", "roles", fixture.name+".go")
			directory := root
			if strings.HasPrefix(fixture.name, "external_") {
				directory = t.TempDir()
				code, err := os.ReadFile(source)
				if err != nil {
					t.Fatal(err)
				}
				source = filepath.Join(directory, "main.go")
				if err := os.WriteFile(source, code, 0600); err != nil {
					t.Fatal(err)
				}
				mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
				if err != nil {
					t.Fatal(err)
				}
				module := strings.Replace(string(mod), "module github.com/sshaplygin/go-socket.io", "module example.com/consumer", 1)
				module += "\nrequire github.com/sshaplygin/go-socket.io v0.0.0\nreplace github.com/sshaplygin/go-socket.io => " + strconv.Quote(root) + "\n"
				if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(module), 0600); err != nil {
					t.Fatal(err)
				}
				sums, err := os.ReadFile(filepath.Join(root, "go.sum"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "go.sum"), sums, 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("go", "test", "-mod=readonly", source)
			cmd.Dir = directory
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
