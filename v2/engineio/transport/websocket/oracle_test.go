package websocket

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestNodeOracle runs testdata/reference/verify-framing.mjs, which drives an echo
// server built on Transport with the ws@8.18.3 client. It is skipped unless Node
// and the pinned dependencies are installed (see testdata/README.md); set
// REQUIRE_NODE_ORACLE=1 to fail instead of skipping.
func TestNodeOracle(t *testing.T) {
	skip := func(format string, args ...any) {
		t.Helper()
		if os.Getenv("REQUIRE_NODE_ORACLE") != "" {
			t.Fatalf(format, args...)
		}
		t.Skipf(format, args...)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		skip("node is not installed")
	}
	if _, err := os.Stat("testdata/reference/node_modules/ws"); err != nil {
		skip("run npm ci --ignore-scripts in testdata/reference first")
	}

	tr := &Transport{MaxPayload: 64}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := tr.Accept(w, r)
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
		for {
			ft, pt, rd, err := c.NextReader()
			if err != nil {
				return
			}
			var data bytes.Buffer
			_, _ = data.ReadFrom(rd)
			_ = rd.Close()
			wr, err := c.NextWriter(ft, pt)
			if err != nil {
				return
			}
			_, _ = wr.Write(data.Bytes())
			if wr.Close() != nil {
				return
			}
		}
	}))
	defer srv.Close()

	cmd := exec.Command(node, "verify-framing.mjs", strings.TrimPrefix(srv.URL, "http://"))
	cmd.Dir = "testdata/reference"
	out, err := cmd.CombinedOutput()
	t.Logf("%s", out)
	require.NoError(t, err)
	require.Contains(t, string(out), "Verified")
}
